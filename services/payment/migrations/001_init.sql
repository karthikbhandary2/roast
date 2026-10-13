-- Payment Service database
-- Owns charges, refunds, the seller payout ledger, and payouts themselves.
-- This is the money-critical database in the system, so it leans heavily on
-- an append-only ledger rather than mutable running totals.

CREATE EXTENSION IF NOT EXISTS pgcrypto;

CREATE TYPE charge_status AS ENUM ('pending', 'succeeded', 'failed', 'refunded');

-- Closed set of 4 supported currencies, matching common.proto's CurrencyCode.
-- JPY has no minor unit - see the proto comment; amount_amount_minor for a
-- JPY charge is the whole-yen amount, not yen*100.
CREATE TYPE currency_code AS ENUM ('USD', 'INR', 'GBP', 'JPY');

-- Matches common.proto's PaymentProvider. A charge's provider always equals
-- the seller's onboarded provider (seller_profiles.provider in User
-- Service) - duplicated here, not looked up live, because a charge is a
-- historical record and must stay correct even if the seller later changes
-- provider for FUTURE sales (re-onboarding, not an in-place update - see
-- user.proto).
CREATE TYPE payment_provider AS ENUM ('razorpay', 'stripe');

CREATE TABLE charges (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    sub_order_id UUID NOT NULL UNIQUE, -- exactly one charge per sub-order
    buyer_id UUID NOT NULL,
    seller_id UUID NOT NULL,
    provider payment_provider NOT NULL,
    amount_currency currency_code NOT NULL DEFAULT 'USD',
    amount_amount_minor BIGINT NOT NULL,
    platform_fee_amount_minor BIGINT NOT NULL,
    seller_share_amount_minor BIGINT NOT NULL,
    status charge_status NOT NULL DEFAULT 'pending',
    provider_charge_id TEXT, -- id from the payment provider (Razorpay payment id / Stripe PaymentIntent id), set once their webhook confirms
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    CHECK (platform_fee_amount_minor + seller_share_amount_minor = amount_amount_minor),
    -- Razorpay only ever charges INR; Stripe only ever charges USD/GBP/JPY
    -- in this system. A row that violates this means the routing logic in
    -- the service layer has a bug - this CHECK exists to catch that at
    -- write time instead of discovering it later in a reconciliation report.
    CHECK (
      (provider = 'razorpay' AND amount_currency = 'INR') OR
      (provider = 'stripe' AND amount_currency IN ('USD', 'GBP', 'JPY'))
    )
);

CREATE INDEX idx_charges_seller ON charges(seller_id);
-- provider_charge_id is only unique WITHIN a provider's own id space, so the
-- dedupe index is scoped per provider, not global.
CREATE UNIQUE INDEX idx_charges_provider_id ON charges(provider, provider_charge_id) WHERE provider_charge_id IS NOT NULL;

CREATE TABLE refunds (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    charge_id UUID NOT NULL REFERENCES charges(id),
    provider payment_provider NOT NULL, -- must match charges.provider - a refund always goes back through the same provider that collected the charge
    amount_currency currency_code NOT NULL DEFAULT 'USD', -- should match the parent charge's currency; enforce in the service layer, not worth a cross-row CHECK
    amount_amount_minor BIGINT NOT NULL,
    reason TEXT,
    status TEXT NOT NULL DEFAULT 'pending', -- 'pending' | 'succeeded' | 'failed'
    provider_refund_id TEXT,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX idx_refunds_charge ON refunds(charge_id);

-- Append-only ledger: the only source of truth for what a seller is owed.
-- Never UPDATE a running balance in place - always INSERT a signed entry,
-- so "how much does this seller have" is always a SUM() over an auditable,
-- replayable history rather than a number that can silently drift.
CREATE TYPE ledger_entry_type AS ENUM ('sale', 'platform_fee', 'refund', 'payout');

-- seller_id '00000000-0000-0000-0000-000000000001' is a RESERVED constant
-- representing the platform itself, not a real seller. Every ChargePayment
-- writes exactly two entries: a 'sale' credit to the actual seller, and a
-- 'platform_fee' credit to this constant - so SUM(amount_amount_minor) over
-- the whole table always equals total money processed, which is the
-- reconciliation invariant this design is for. See LLD.md §2.5/§9 item 7.
CREATE TABLE ledger_entries (
    id BIGSERIAL PRIMARY KEY,
    seller_id UUID NOT NULL, -- or the platform constant above
    sub_order_id UUID,
    payout_id UUID,
    entry_type ledger_entry_type NOT NULL,
    amount_amount_minor BIGINT NOT NULL, -- signed: positive credits the seller, negative debits (e.g. a refund or a payout)
    created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX idx_ledger_seller ON ledger_entries(seller_id, created_at);

-- Whether a 'sale' ledger entry counts as pending or available is NOT a
-- mutable column on ledger_entries (that would break its append-only
-- invariant) - it's whether a row exists here. Populated when
-- order.sub_order_confirmed arrives (see LLD.md §2.5 item 2); a sub_order
-- with no row here is still pending. One row per sub_order, written once,
-- never updated.
CREATE TABLE sub_order_earnings_confirmed (
    sub_order_id UUID PRIMARY KEY,
    confirmed_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

-- Cached/materialized rollup of ledger_entries, refreshed in the same
-- transaction as each new ledger row, so GetSellerBalance is an O(1) read
-- instead of summing the whole ledger on every call. If these two ever
-- disagree, ledger_entries is correct and this table gets rebuilt from it.
CREATE TABLE seller_balances (
    seller_id UUID PRIMARY KEY,
    available_amount_minor BIGINT NOT NULL DEFAULT 0,
    pending_amount_minor BIGINT NOT NULL DEFAULT 0,
    currency currency_code NOT NULL DEFAULT 'USD',
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE TABLE payouts (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    seller_id UUID NOT NULL,
    provider payment_provider NOT NULL, -- always the seller's onboarded provider
    amount_currency currency_code NOT NULL DEFAULT 'USD',
    amount_amount_minor BIGINT NOT NULL,
    method TEXT NOT NULL,
    status TEXT NOT NULL DEFAULT 'pending', -- 'pending' | 'paid' | 'failed'
    provider_payout_id TEXT,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX idx_payouts_seller ON payouts(seller_id, created_at DESC);

CREATE TABLE idempotency_keys (
    key TEXT PRIMARY KEY,
    request_type TEXT NOT NULL, -- 'charge' | 'refund'
    response_payload JSONB NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

-- Dedupe table for the payment providers' webhook deliveries - providers
-- redeliver on timeout or ambiguous response, so the same event id can
-- legitimately arrive more than once and must be a safe no-op on replay.
-- Keyed by (provider, provider_event_id), NOT provider_event_id alone - two
-- independent providers' event-id spaces are not guaranteed disjoint, and a
-- global PRIMARY KEY on the id alone would (very unlikely, but needlessly)
-- risk treating a Razorpay event and a same-valued Stripe event id as the
-- same row.
CREATE TABLE webhook_events (
    provider payment_provider NOT NULL,
    provider_event_id TEXT NOT NULL,
    event_type TEXT NOT NULL,
    payload JSONB NOT NULL,
    processed_at TIMESTAMPTZ,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    PRIMARY KEY (provider, provider_event_id)
);
