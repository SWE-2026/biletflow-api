ALTER TABLE audit_log ALTER COLUMN entity_id DROP NOT NULL;

CREATE TABLE disputes (
  id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
  order_id UUID NOT NULL REFERENCES orders(id),
  opened_by UUID NOT NULL REFERENCES users(id),
  reason TEXT NOT NULL CHECK (reason IN ('not_received', 'event_cancelled', 'not_as_described',
    'duplicate_charge', 'other')),
  details TEXT NOT NULL DEFAULT '',
  status TEXT NOT NULL DEFAULT 'open' CHECK (status IN ('open', 'refunded', 'rejected')),
  resolution_note TEXT NOT NULL DEFAULT '',
  created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
  resolved_at TIMESTAMPTZ,
  resolved_by UUID REFERENCES users(id)
);
CREATE UNIQUE INDEX idx_disputes_open ON disputes (order_id) WHERE status = 'open';
CREATE INDEX idx_disputes_status ON disputes (status, created_at DESC);
