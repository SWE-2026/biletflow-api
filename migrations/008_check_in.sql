ALTER TABLE event_staff ADD CONSTRAINT event_staff_role_check CHECK (role IN ('scanner', 'supervisor'));
ALTER TABLE event_staff ADD COLUMN added_by UUID REFERENCES users(id);
CREATE INDEX idx_event_staff_user ON event_staff (user_id);

CREATE INDEX idx_check_ins_ticket ON check_ins (ticket_id);
CREATE UNIQUE INDEX idx_check_ins_active ON check_ins (ticket_id) WHERE reversed_at IS NULL;
