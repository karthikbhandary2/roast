-- Product Service database
-- Owns the catalog and is the single source of truth for inventory. This is
-- the database the checkout saga's "reserve inventory" step reads/writes.

CREATE EXTENSION IF NOT EXISTS pgcrypto;

CREATE TYPE product_status AS ENUM ('draft', 'live', 'low_stock', 'sold_out');

-- Closed set of 4 supported currencies, matching common.proto's CurrencyCode.
-- Note JPY has no minor unit (see the proto comment) - price_amount_minor
-- for a JPY-priced product is the whole-yen amount, not yen*100.
--
-- CROSS-SERVICE RULE (not enforceable as a DB constraint here, since
-- seller_profiles.provider lives in User Service's own database): a seller
-- onboarded with Razorpay may only list in INR; a seller onboarded with
-- Stripe may only list in USD/GBP/JPY. CreateListing/UpdateListing in the
-- service layer must call User Service to check the seller's provider and
-- reject a mismatched price_currency with codes.FailedPrecondition. See
-- LLD.md Product Service and Payment Service sections.
CREATE TYPE currency_code AS ENUM ('USD', 'INR', 'GBP', 'JPY');

CREATE TABLE products (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    seller_id UUID NOT NULL, -- reference to User Service's users.id; no FK across service databases
    name TEXT NOT NULL,
    description TEXT NOT NULL DEFAULT '',
    price_currency currency_code NOT NULL DEFAULT 'USD',
    price_amount_minor BIGINT NOT NULL CHECK (price_amount_minor >= 0),
    batch_label TEXT,
    stock_total INT NOT NULL DEFAULT 0 CHECK (stock_total >= 0),
    stock_reserved INT NOT NULL DEFAULT 0 CHECK (stock_reserved >= 0),
    status product_status NOT NULL DEFAULT 'draft',
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    CHECK (stock_reserved <= stock_total)
);
-- stock_available (shown to buyers) = stock_total - stock_reserved; derived,
-- never stored, so it can't drift out of sync with the two counts above.

CREATE INDEX idx_products_seller ON products(seller_id);
CREATE INDEX idx_products_status ON products(status) WHERE status IN ('live', 'low_stock');

-- Separate table (not a TEXT[] column) so position/ordering is explicit and
-- future per-image metadata (alt text, a moderation status) has somewhere
-- to live without a schema migration. Images are uploaded directly to
-- object storage by the client via a presigned URL (GetImageUploadURL) -
-- this table only ever stores the resulting public URLs, never file bytes.
CREATE TABLE product_images (
    product_id UUID NOT NULL REFERENCES products(id) ON DELETE CASCADE,
    position SMALLINT NOT NULL, -- 0 = primary/card image
    url TEXT NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    PRIMARY KEY (product_id, position)
);

CREATE TYPE reservation_status AS ENUM ('active', 'confirmed', 'released');

CREATE TABLE inventory_reservations (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    sub_order_id UUID NOT NULL UNIQUE, -- exactly one reservation per sub-order
    seller_id UUID NOT NULL,
    status reservation_status NOT NULL DEFAULT 'active',
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    expires_at TIMESTAMPTZ NOT NULL -- reservation-hold TTL; the cart's countdown timer reads this
);

CREATE TABLE reservation_lines (
    reservation_id UUID NOT NULL REFERENCES inventory_reservations(id) ON DELETE CASCADE,
    product_id UUID NOT NULL REFERENCES products(id),
    quantity INT NOT NULL CHECK (quantity > 0),
    PRIMARY KEY (reservation_id, product_id)
);

CREATE INDEX idx_reservation_lines_product ON reservation_lines(product_id);
CREATE INDEX idx_reservations_expiry ON inventory_reservations(expires_at) WHERE status = 'active';
-- ^ a background sweeper polls this index to auto-release reservations whose
-- hold has expired without a confirming payment (abandoned checkout).

-- Idempotency ledger for ReserveInventory / ReleaseInventory / ConfirmReservation.
-- The application MUST write the row here in the SAME transaction as the
-- stock mutation it protects, so a retried call with the same key is
-- guaranteed to observe either both effects or neither - never one without
-- the other.
CREATE TABLE idempotency_keys (
    key TEXT PRIMARY KEY,
    request_type TEXT NOT NULL, -- 'reserve' | 'release' | 'confirm'
    response_payload JSONB NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

-- Reservation writes should SELECT ... FOR UPDATE the affected product
-- row(s) (ordered by product_id to avoid deadlocks between two multi-item
-- reservations) before checking stock_total - stock_reserved >= quantity,
-- to prevent two concurrent reservations from both succeeding on the last
-- unit of a batch.

-- Reviews. Product Service owns these (co-located with the product detail
-- page that displays them) even though it doesn't own purchase history -
-- CreateReview verifies eligibility by calling OrderService.VerifyPurchase
-- at write time (see product.proto and LLD.md); sub_order_id here is just
-- the resulting proof-of-purchase reference, not independently re-verified
-- on every read.
CREATE TABLE reviews (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    product_id UUID NOT NULL REFERENCES products(id) ON DELETE CASCADE,
    buyer_id UUID NOT NULL,
    sub_order_id UUID NOT NULL,
    rating SMALLINT NOT NULL CHECK (rating BETWEEN 1 AND 5),
    comment TEXT NOT NULL DEFAULT '',
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now(), -- bumped by UpdateReview
    -- one review per buyer per product, matching the standard e-commerce
    -- pattern (Amazon, etc.) - a buyer who re-purchases edits their
    -- existing review (an UPDATE, via a not-yet-designed UpdateReview RPC)
    -- rather than accumulating multiple reviews for the same product
    UNIQUE (buyer_id, product_id)
);

CREATE INDEX idx_reviews_product ON reviews(product_id, created_at DESC);
