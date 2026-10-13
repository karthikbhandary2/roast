-- Order Service database
-- Owns checkout orchestration: one order per checkout, split into one
-- sub-order per seller. The cart itself is NOT in this schema - it lives in
-- Redis as a hash at key `cart:{buyer_id}` with a TTL matching the
-- reservation hold window, so it stays out of Postgres entirely until
-- checkout is confirmed.

CREATE EXTENSION IF NOT EXISTS pgcrypto;

CREATE TYPE order_status AS ENUM ('processing', 'confirmed', 'partially_failed', 'failed');
-- 'awaiting_payment': charge RPC returned PENDING (e.g. an unapproved UPI
-- collect request) - the saga is suspended, not failed, until a webhook or
-- the timeout sweep resolves it. See LLD.md Order Service section.
CREATE TYPE sub_order_status AS ENUM ('reserving', 'charging', 'awaiting_payment', 'confirmed', 'compensating', 'failed', 'shipped', 'delivered');

-- Closed set of 4 supported currencies, matching common.proto's CurrencyCode.
CREATE TYPE currency_code AS ENUM ('USD', 'INR', 'GBP', 'JPY');

CREATE TABLE orders (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    buyer_id UUID NOT NULL,
    shipping_address JSONB NOT NULL,
    total_currency currency_code NOT NULL DEFAULT 'USD',
    total_amount_minor BIGINT NOT NULL,
    status order_status NOT NULL DEFAULT 'processing',
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX idx_orders_buyer ON orders(buyer_id, created_at DESC);

CREATE TABLE sub_orders (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    parent_order_id UUID NOT NULL REFERENCES orders(id) ON DELETE CASCADE,
    seller_id UUID NOT NULL,
    subtotal_currency currency_code NOT NULL DEFAULT 'USD',
    subtotal_amount_minor BIGINT NOT NULL,
    status sub_order_status NOT NULL DEFAULT 'reserving',
    reservation_id UUID, -- set once Product Service confirms the inventory reservation
    charge_id UUID,      -- set once Payment Service confirms the charge
    tracking_number TEXT,
    failure_reason TEXT,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX idx_sub_orders_parent ON sub_orders(parent_order_id);
CREATE INDEX idx_sub_orders_seller ON sub_orders(seller_id, status, created_at DESC);
-- ^ the seller order-fulfillment screen filters/sorts on exactly this shape.

CREATE TABLE sub_order_items (
    sub_order_id UUID NOT NULL REFERENCES sub_orders(id) ON DELETE CASCADE,
    product_id UUID NOT NULL,
    quantity INT NOT NULL CHECK (quantity > 0),
    unit_price_currency currency_code NOT NULL DEFAULT 'USD',
    unit_price_amount_minor BIGINT NOT NULL,
    PRIMARY KEY (sub_order_id, product_id)
);

-- Append-only saga event log - the "event sourcing basics" piece of this
-- project. sub_orders.status is a projection derived from this log, not the
-- other way around: every step transition (including failures and
-- compensations) gets its own row here, so the full saga history for a
-- sub-order can be replayed for debugging without depending on whatever the
-- current status column happens to say.
CREATE TABLE saga_events (
    id BIGSERIAL PRIMARY KEY,
    sub_order_id UUID NOT NULL REFERENCES sub_orders(id) ON DELETE CASCADE,
    step TEXT NOT NULL,   -- 'reserve_inventory' | 'charge_payment' | 'confirm_order' | 'notify'
    status TEXT NOT NULL, -- 'started' | 'completed' | 'failed' | 'compensated'
    payload JSONB,
    correlation_id UUID NOT NULL, -- ties every step of one sub-order's saga together across services' logs/traces
    created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX idx_saga_events_sub_order ON saga_events(sub_order_id, created_at);
CREATE INDEX idx_saga_events_correlation ON saga_events(correlation_id);

CREATE TABLE idempotency_keys (
    key TEXT PRIMARY KEY, -- e.g. a client-generated checkout request key, to dedupe double-clicks on "place order"
    request_type TEXT NOT NULL,
    response_payload JSONB NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

-- A shipping carrier's webhook looks up the sub-order by tracking_number
-- (it has no idea about our sub_order_id), so this index is what makes that
-- lookup fast rather than a full scan.
CREATE INDEX idx_sub_orders_tracking_number ON sub_orders(tracking_number) WHERE tracking_number IS NOT NULL;

-- Dedupe table for shipping-carrier webhook deliveries - same pattern as
-- payment_db.webhook_events, and for the same reason (carriers redeliver).
-- Generic/carrier-agnostic on purpose: this project doesn't commit to a
-- specific carrier's API shape, just the dedupe mechanics any of them need.
-- See LLD.md Order Service "Delivery confirmation".
CREATE TABLE shipping_webhook_events (
    carrier TEXT NOT NULL,       -- e.g. "shiprocket", "delhivery" - whatever is actually integrated later
    provider_event_id TEXT NOT NULL,
    event_type TEXT NOT NULL,
    payload JSONB NOT NULL,
    processed_at TIMESTAMPTZ,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    PRIMARY KEY (carrier, provider_event_id)
);
