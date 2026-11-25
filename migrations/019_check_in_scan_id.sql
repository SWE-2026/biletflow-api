-- Scanner-generated ID of the scan that created a check-in. A scanner that lost the response to a
-- scan (or replays scans made offline) sends the same ID again and gets the first outcome back.
ALTER TABLE check_ins ADD COLUMN scan_id UUID;
CREATE UNIQUE INDEX idx_check_ins_scan_id ON check_ins (scan_id) WHERE scan_id IS NOT NULL;
