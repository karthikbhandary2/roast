-- Notification Service database
-- Deliberately small: this service's job is to send things, not model
-- business state. The notifications table doubles as both an audit log and
-- the dedupe mechanism (a retried "send order confirmation" for the same
-- order and recipient is a safe no-op, via the unique constraint below).

CREATE EXTENSION IF NOT EXISTS pgcrypto;

CREATE TYPE notification_channel AS ENUM ('email', 'sms');
CREATE TYPE notification_status AS ENUM ('queued', 'sent', 'failed');

CREATE TABLE notifications (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    recipient_user_id UUID NOT NULL,
    channel notification_channel NOT NULL DEFAULT 'email',
    notification_type TEXT NOT NULL, -- 'order_confirmation' | 'shipping_update' | 'low_stock_alert' | 'payout_notification'
    related_entity_id UUID NOT NULL, -- order_id, sub_order_id, product_id, or payout_id, depending on notification_type
    status notification_status NOT NULL DEFAULT 'queued',
    attempts INT NOT NULL DEFAULT 0,
    last_error TEXT,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    sent_at TIMESTAMPTZ,
    UNIQUE (notification_type, related_entity_id, recipient_user_id)
);

CREATE INDEX idx_notifications_queued ON notifications(created_at) WHERE status = 'queued';
-- ^ a worker polls this index and retries with backoff on failure, per the
-- saga's "step 4 is retried, not compensated" rule.
