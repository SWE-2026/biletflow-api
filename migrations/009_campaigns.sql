CREATE TABLE campaigns (
  id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
  event_id UUID NOT NULL REFERENCES events(id),
  name TEXT NOT NULL CHECK (btrim(name) <> ''),
  code TEXT NOT NULL,
  token TEXT NOT NULL UNIQUE,
  discount_type TEXT NOT NULL CHECK (discount_type IN ('percent', 'fixed')),
  discount_value BIGINT NOT NULL CHECK (discount_value > 0),
  starts_at TIMESTAMPTZ,
  ends_at TIMESTAMPTZ,
  max_redemptions INTEGER CHECK (max_redemptions > 0),
  status TEXT NOT NULL DEFAULT 'active' CHECK (status IN ('active', 'disabled')),
  created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
  updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
  CHECK (discount_type <> 'percent' OR discount_value <= 100),
  CHECK (starts_at IS NULL OR ends_at IS NULL OR ends_at > starts_at)
);
CREATE UNIQUE INDEX idx_campaigns_event_code ON campaigns (event_id, code);

CREATE TABLE campaign_ticket_types (
  campaign_id UUID NOT NULL REFERENCES campaigns(id) ON DELETE CASCADE,
  ticket_type_id UUID NOT NULL REFERENCES ticket_types(id) ON DELETE CASCADE,
  PRIMARY KEY (campaign_id, ticket_type_id)
);

ALTER TABLE orders ADD COLUMN campaign_id UUID REFERENCES campaigns(id);
ALTER TABLE orders ADD COLUMN subtotal_amount BIGINT NOT NULL DEFAULT 0 CHECK (subtotal_amount >= 0);
ALTER TABLE orders ADD COLUMN discount_amount BIGINT NOT NULL DEFAULT 0 CHECK (discount_amount >= 0);
UPDATE orders SET subtotal_amount = total_amount;
ALTER TABLE orders ADD CONSTRAINT orders_discounted_total CHECK (total_amount = subtotal_amount - discount_amount);
CREATE INDEX idx_orders_campaign ON orders (campaign_id, status) WHERE campaign_id IS NOT NULL;
