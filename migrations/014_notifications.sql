CREATE TABLE notifications (
  id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
  user_id UUID NOT NULL REFERENCES users(id),
  kind TEXT NOT NULL CHECK (kind IN ('order_confirmed', 'tickets_delivered', 'payment_failed',
    'event_updated', 'event_cancelled', 'registration_cancelled', 'refund_completed', 'payout_status',
    'support_message', 'support_assigned', 'support_status')),
  title TEXT NOT NULL,
  body TEXT NOT NULL,
  link TEXT NOT NULL DEFAULT '',
  email_to TEXT NOT NULL,
  order_id UUID REFERENCES orders(id),
  event_id UUID REFERENCES events(id),
  read_at TIMESTAMPTZ,
  email_status TEXT NOT NULL DEFAULT 'pending' CHECK (email_status IN ('pending', 'sent', 'failed')),
  email_attempts INTEGER NOT NULL DEFAULT 0,
  emailed_at TIMESTAMPTZ,
  created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE INDEX idx_notifications_user ON notifications (user_id, created_at DESC);
CREATE INDEX idx_notifications_outbox ON notifications (created_at) WHERE email_status = 'pending';
