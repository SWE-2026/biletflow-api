ALTER TABLE events ADD CONSTRAINT events_capacity_max CHECK (capacity <= 1000000);
ALTER TABLE events ADD CONSTRAINT events_registration_before_start CHECK (
  (registration_opens_at IS NULL OR registration_opens_at < starts_at)
  AND (registration_closes_at IS NULL OR registration_closes_at < starts_at)
);

CREATE INDEX idx_events_organizer ON events (organizer_id, created_at DESC);
DROP INDEX idx_events_public;
CREATE INDEX idx_events_public ON events (starts_at) WHERE status = 'published' AND visibility = 'public';

ALTER TABLE ticket_types ADD COLUMN description TEXT NOT NULL DEFAULT '';
ALTER TABLE ticket_types ADD COLUMN sales_starts_at TIMESTAMPTZ;
ALTER TABLE ticket_types ADD COLUMN sales_ends_at TIMESTAMPTZ;
ALTER TABLE ticket_types ADD COLUMN max_per_order INTEGER NOT NULL DEFAULT 10
  CHECK (max_per_order BETWEEN 1 AND 100);
ALTER TABLE ticket_types ADD COLUMN hidden BOOLEAN NOT NULL DEFAULT false;
ALTER TABLE ticket_types ADD COLUMN sort_order INTEGER NOT NULL DEFAULT 0;
ALTER TABLE ticket_types ADD COLUMN updated_at TIMESTAMPTZ NOT NULL DEFAULT now();
ALTER TABLE ticket_types ADD CONSTRAINT ticket_types_name_present CHECK (btrim(name) <> '');
ALTER TABLE ticket_types ADD CONSTRAINT ticket_types_sales_window CHECK (
  sales_starts_at IS NULL OR sales_ends_at IS NULL OR sales_ends_at > sales_starts_at
);

CREATE INDEX idx_ticket_types_event ON ticket_types (event_id, sort_order, created_at);
