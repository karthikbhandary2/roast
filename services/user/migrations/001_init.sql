-- User Service database
-- Owns identity for buyers, sellers, and admins (one users table, roles are
-- additive - a person can be a buyer and a seller at once).

CREATE EXTENSION IF NOT EXISTS pgcrypto;

CREATE TABLE users (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    email TEXT NOT NULL UNIQUE,
    password_hash TEXT NOT NULL,
    display_name TEXT NOT NULL,
    -- NULL = no restriction. Set to now() on Logout (self-service, "log out
    -- everywhere") or when UpdateSellerStatus suspends/rejects a seller
    -- (forced revocation). An access token whose iat claim predates this
    -- is rejected for seller/admin-gated actions - see LLD.md §2.2 "Token
    -- revocation". Deliberately NOT checked on every request (would
    -- reintroduce the per-request DB hop §4.3 avoids) - only on
    -- seller/admin-gated RPCs, via a short-TTL cache.
    tokens_valid_after TIMESTAMPTZ,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE TYPE user_role AS ENUM ('buyer', 'seller', 'admin');

CREATE TABLE user_roles (
    user_id UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    role user_role NOT NULL,
    PRIMARY KEY (user_id, role)
);

CREATE TYPE seller_status AS ENUM ('pending_review', 'active', 'suspended', 'rejected');

-- The two payment processors this platform integrates with. Chosen once at
-- onboarding and immutable thereafter (see user.proto's RegisterSellerRequest
-- comment) - it determines which currency this seller may list in:
-- razorpay -> INR only, stripe -> USD/GBP/JPY. Enforced at listing-creation
-- time in Product Service (cross-service rule, not a DB constraint here).
CREATE TYPE payment_provider AS ENUM ('razorpay', 'stripe');

-- A seller profile is always an extension of a user row, never a standalone
-- identity - hence the 1:1 PK/FK on user_id rather than its own surrogate key.
CREATE TABLE seller_profiles (
    user_id UUID PRIMARY KEY REFERENCES users(id) ON DELETE CASCADE,
    business_name TEXT NOT NULL,
    provider payment_provider NOT NULL,
    payout_method TEXT NOT NULL,
    payout_account_ref TEXT NOT NULL, -- opaque token from the payment provider; never a raw bank account/routing number
    commission_rate NUMERIC(5, 4) NOT NULL DEFAULT 0.10 CHECK (commission_rate >= 0 AND commission_rate < 1),
    status seller_status NOT NULL DEFAULT 'pending_review',
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

-- Append-only audit trail of every admin decision on a seller application,
-- same "log the transition, don't just overwrite the current state" pattern
-- as order_db.saga_events - lets support answer "who approved this seller
-- and why" without relying on seller_profiles only holding the latest state.
CREATE TABLE seller_status_history (
    id BIGSERIAL PRIMARY KEY,
    user_id UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    old_status seller_status,
    new_status seller_status NOT NULL,
    reason TEXT,
    changed_by_admin_id UUID, -- NULL for the initial pending_review insert (the seller's own action, not an admin's)
    created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX idx_seller_status_history_user ON seller_status_history(user_id, created_at DESC);

CREATE TABLE refresh_tokens (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    user_id UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    token_hash TEXT NOT NULL UNIQUE,
    expires_at TIMESTAMPTZ NOT NULL,
    revoked_at TIMESTAMPTZ,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX idx_refresh_tokens_user ON refresh_tokens(user_id);
CREATE INDEX idx_seller_profiles_status ON seller_profiles(status) WHERE status = 'pending_review';
