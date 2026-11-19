ALTER TABLE events ADD COLUMN seating_mode TEXT NOT NULL DEFAULT 'general'
  CHECK (seating_mode IN ('general', 'assigned'));
ALTER TABLE events ADD COLUMN seating_layout TEXT;
ALTER TABLE events ADD CONSTRAINT events_seating_layout CHECK ((seating_mode = 'assigned') = (seating_layout IS NOT NULL));

CREATE TABLE seats (
  id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
  event_id UUID NOT NULL REFERENCES events(id) ON DELETE CASCADE,
  section TEXT NOT NULL,
  row_label TEXT NOT NULL,
  number INTEGER NOT NULL CHECK (number > 0),
  category TEXT NOT NULL,
  accessible BOOLEAN NOT NULL DEFAULT false,
  ticket_type_id UUID REFERENCES ticket_types(id) ON DELETE SET NULL,
  sort_order INTEGER NOT NULL,
  UNIQUE (event_id, section, row_label, number)
);
CREATE INDEX idx_seats_event ON seats (event_id, sort_order);

CREATE TABLE seat_holds (
  order_id UUID NOT NULL REFERENCES orders(id) ON DELETE CASCADE,
  seat_id UUID NOT NULL REFERENCES seats(id),
  order_item_id UUID NOT NULL REFERENCES order_items(id) ON DELETE CASCADE,
  created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
  PRIMARY KEY (order_id, seat_id)
);
CREATE INDEX idx_seat_holds_seat ON seat_holds (seat_id);
CREATE INDEX idx_seat_holds_item ON seat_holds (order_item_id);

ALTER TABLE tickets ADD COLUMN seat_id UUID REFERENCES seats(id);
CREATE UNIQUE INDEX idx_tickets_seat_taken ON tickets (seat_id)
  WHERE seat_id IS NOT NULL AND status IN ('valid', 'used');
