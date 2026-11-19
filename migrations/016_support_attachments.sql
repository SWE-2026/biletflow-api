CREATE TABLE support_attachments (
  id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
  case_id UUID NOT NULL REFERENCES support_cases(id),
  message_id UUID NOT NULL REFERENCES support_messages(id),
  file_name TEXT NOT NULL CHECK (btrim(file_name) <> ''),
  content_type TEXT NOT NULL,
  size_bytes INTEGER NOT NULL CHECK (size_bytes > 0),
  data BYTEA NOT NULL,
  created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE INDEX idx_support_attachments_message ON support_attachments (message_id);
CREATE INDEX idx_support_attachments_case ON support_attachments (case_id);
