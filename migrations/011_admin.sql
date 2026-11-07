ALTER TABLE users ADD COLUMN suspended_at TIMESTAMPTZ;

ALTER TABLE events DROP CONSTRAINT events_status_check;
ALTER TABLE events ADD CONSTRAINT events_status_check
  CHECK (status IN ('draft', 'published', 'cancelled', 'suspended'));

CREATE TABLE platform_settings (
  id BOOLEAN PRIMARY KEY DEFAULT true CHECK (id),
  activation_fee_amount BIGINT NOT NULL DEFAULT 10000 CHECK (activation_fee_amount BETWEEN 0 AND 10000000),
  processing_fee_percent INTEGER NOT NULL DEFAULT 3 CHECK (processing_fee_percent BETWEEN 0 AND 20),
  updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
  updated_by UUID REFERENCES users(id)
);
INSERT INTO platform_settings DEFAULT VALUES;

CREATE TABLE event_reports (
  id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
  event_id UUID NOT NULL REFERENCES events(id),
  reporter_id UUID NOT NULL REFERENCES users(id),
  reason TEXT NOT NULL CHECK (reason IN ('fraud', 'inappropriate', 'misleading', 'other')),
  details TEXT NOT NULL DEFAULT '',
  status TEXT NOT NULL DEFAULT 'open' CHECK (status IN ('open', 'dismissed', 'actioned')),
  created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
  reviewed_at TIMESTAMPTZ,
  reviewed_by UUID REFERENCES users(id)
);
CREATE UNIQUE INDEX idx_event_reports_open ON event_reports (event_id, reporter_id) WHERE status = 'open';
CREATE INDEX idx_event_reports_status ON event_reports (status, created_at DESC);

CREATE INDEX idx_payments_status ON payments (status, created_at DESC);
CREATE INDEX idx_refunds_created ON refunds (created_at DESC);
