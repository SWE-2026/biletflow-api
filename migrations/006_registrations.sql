-- Lookups for registration: sold counts per ticket type and an attendee's own tickets.
CREATE INDEX idx_order_items_ticket_type ON order_items (ticket_type_id);
CREATE INDEX idx_tickets_order_item ON tickets (order_item_id);
CREATE INDEX idx_tickets_owner ON tickets (owner_id, created_at DESC);
