CREATE EXTENSION IF NOT EXISTS pgcrypto;

CREATE TABLE organizers (
  id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
  type TEXT NOT NULL CHECK (type IN ('individual', 'business')),
  created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE TABLE users (
  id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
  organizer_id UUID REFERENCES organizers(id),
  email TEXT NOT NULL UNIQUE,
  display_name TEXT NOT NULL,
  password_hash TEXT NOT NULL,
  role TEXT NOT NULL DEFAULT 'customer' CHECK (role IN ('customer', 'organizer', 'staff', 'admin')),
  created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE TABLE events (
  id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
  title TEXT NOT NULL,
  description TEXT NOT NULL DEFAULT '',
  starts_at TIMESTAMPTZ NOT NULL,
  ends_at TIMESTAMPTZ NOT NULL,
  status TEXT NOT NULL DEFAULT 'draft' CHECK (status IN ('draft', 'published', 'cancelled')),
  image_url TEXT,
  created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
  organizer_id UUID NOT NULL REFERENCES organizers(id),
  CHECK (ends_at > starts_at)
);

CREATE TABLE ticket_types (
  id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
  name TEXT NOT NULL,
  price_amount BIGINT NOT NULL CHECK (price_amount >= 0),
  quantity INTEGER NOT NULL CHECK (quantity >= 0),
  created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
  event_id UUID NOT NULL REFERENCES events(id) ON DELETE CASCADE
);

CREATE TABLE orders (
  id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
  status TEXT NOT NULL DEFAULT 'pending' CHECK (status IN ('pending', 'paid', 'cancelled', 'refunded')),
  total_amount BIGINT NOT NULL CHECK (total_amount >= 0),
  created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
  event_id UUID NOT NULL REFERENCES events(id),
  buyer_id UUID NOT NULL REFERENCES users(id)
);

CREATE TABLE order_items (
  id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
  order_id UUID NOT NULL REFERENCES orders(id) ON DELETE CASCADE,
  ticket_type_id UUID NOT NULL REFERENCES ticket_types(id),
  quantity INTEGER NOT NULL CHECK (quantity > 0),
  unit_price_amount BIGINT NOT NULL CHECK (unit_price_amount >= 0),
  created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE TABLE payments (
  id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
  order_id UUID NOT NULL REFERENCES orders(id),
  amount BIGINT NOT NULL CHECK (amount >= 0),
  status TEXT NOT NULL CHECK (status IN ('pending', 'succeeded', 'failed', 'refunded')),
  provider TEXT NOT NULL,
  provider_payment_id TEXT,
  created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE TABLE refunds (
  id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
  payment_id UUID NOT NULL REFERENCES payments(id),
  amount BIGINT NOT NULL CHECK (amount > 0),
  status TEXT NOT NULL CHECK (status IN ('pending', 'succeeded', 'failed')),
  reason TEXT,
  provider_refund_id TEXT,
  created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE TABLE tickets (
  id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
  order_item_id UUID NOT NULL REFERENCES order_items(id),
  owner_id UUID NOT NULL REFERENCES users(id),
  qr_token TEXT NOT NULL UNIQUE,
  status TEXT NOT NULL DEFAULT 'valid' CHECK (status IN ('valid', 'used', 'cancelled')),
  created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE TABLE check_ins (
  id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
  checked_in_by UUID REFERENCES users(id),
  reversed_by UUID REFERENCES users(id),
  checked_in_at TIMESTAMPTZ,
  reversed_at TIMESTAMPTZ,
  ticket_id UUID NOT NULL REFERENCES tickets(id)
);

CREATE TABLE event_staff (
  id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
  event_id UUID NOT NULL REFERENCES events(id) ON DELETE CASCADE,
  role TEXT NOT NULL,
  created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
  user_id UUID NOT NULL REFERENCES users(id),
  UNIQUE (event_id, user_id)
);

CREATE TABLE paid_sales_activations (
  id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
  event_id UUID NOT NULL UNIQUE REFERENCES events(id) ON DELETE CASCADE,
  status TEXT NOT NULL CHECK (status IN ('pending', 'active', 'suspended')),
  reviewed_at TIMESTAMPTZ,
  activated_at TIMESTAMPTZ,
  suspended_at TIMESTAMPTZ,
  reviewed_by UUID REFERENCES users(id)
);

CREATE TABLE sessions (
  id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
  user_id UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
  token_hash TEXT NOT NULL UNIQUE,
  expires_at TIMESTAMPTZ NOT NULL,
  revoked_at TIMESTAMPTZ,
  created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX idx_events_public ON events (status, starts_at);
CREATE INDEX idx_sessions_token ON sessions (token_hash) WHERE revoked_at IS NULL;
CREATE INDEX idx_orders_buyer ON orders (buyer_id, created_at DESC);

INSERT INTO organizers (id, type) VALUES ('00000000-0000-0000-0000-000000000001', 'business') ON CONFLICT DO NOTHING;
INSERT INTO events (id, title, description, starts_at, ends_at, status, organizer_id)
VALUES ('00000000-0000-0000-0000-000000000010', 'SWE Launch Night', 'Sample event created by the initial migration.', now() + interval '30 days', now() + interval '30 days 3 hours', 'published', '00000000-0000-0000-0000-000000000001') ON CONFLICT DO NOTHING;
INSERT INTO ticket_types (name, price_amount, quantity, event_id)
SELECT 'General admission', 2500, 100, '00000000-0000-0000-0000-000000000010' WHERE NOT EXISTS (SELECT 1 FROM ticket_types WHERE event_id = '00000000-0000-0000-0000-000000000010');
