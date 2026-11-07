ALTER TABLE events ADD COLUMN refund_policy TEXT NOT NULL DEFAULT '';

ALTER TABLE orders ADD COLUMN attendee_name TEXT NOT NULL DEFAULT '';
ALTER TABLE orders ADD COLUMN attendee_email TEXT NOT NULL DEFAULT '';
ALTER TABLE orders ADD COLUMN processing_fee_amount BIGINT NOT NULL DEFAULT 0
  CHECK (processing_fee_amount >= 0);
ALTER TABLE orders ADD COLUMN expires_at TIMESTAMPTZ;
ALTER TABLE orders ADD COLUMN paid_at TIMESTAMPTZ;
ALTER TABLE orders ADD COLUMN cancelled_at TIMESTAMPTZ;
ALTER TABLE orders ADD COLUMN refunded_at TIMESTAMPTZ;
UPDATE orders SET paid_at = created_at WHERE status = 'paid';
UPDATE orders SET expires_at = created_at WHERE status = 'pending';
ALTER TABLE orders ADD CONSTRAINT orders_pending_expiry CHECK (status <> 'pending' OR expires_at IS NOT NULL);
CREATE INDEX idx_orders_event ON orders (event_id, created_at DESC);

ALTER TABLE tickets DROP CONSTRAINT tickets_status_check;
ALTER TABLE tickets ADD CONSTRAINT tickets_status_check
  CHECK (status IN ('valid', 'used', 'cancelled', 'refunded'));

CREATE INDEX idx_payments_order ON payments (order_id, created_at);
CREATE UNIQUE INDEX idx_payments_one_success ON payments (order_id) WHERE status IN ('succeeded', 'refunded');

ALTER TABLE refunds ADD COLUMN created_by UUID REFERENCES users(id);
CREATE INDEX idx_refunds_payment ON refunds (payment_id);

ALTER TABLE paid_sales_activations ADD COLUMN fee_amount BIGINT NOT NULL DEFAULT 0 CHECK (fee_amount >= 0);
ALTER TABLE paid_sales_activations ADD COLUMN fee_payment_reference TEXT;
ALTER TABLE paid_sales_activations ADD COLUMN terms_accepted_at TIMESTAMPTZ;
ALTER TABLE paid_sales_activations ADD COLUMN activated_by UUID REFERENCES users(id);
ALTER TABLE paid_sales_activations ADD COLUMN created_at TIMESTAMPTZ NOT NULL DEFAULT now();

CREATE TABLE audit_log (
  id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
  event_id UUID REFERENCES events(id),
  actor_id UUID REFERENCES users(id),
  action TEXT NOT NULL,
  entity_type TEXT NOT NULL,
  entity_id UUID NOT NULL,
  details JSONB NOT NULL DEFAULT '{}',
  created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE INDEX idx_audit_log_event ON audit_log (event_id, created_at DESC);
