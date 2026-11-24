ALTER TABLE support_attachments ADD COLUMN storage_key TEXT UNIQUE;
ALTER TABLE support_attachments ALTER COLUMN data DROP NOT NULL;
ALTER TABLE support_attachments ADD CONSTRAINT support_attachments_content
  CHECK (storage_key IS NOT NULL OR data IS NOT NULL);
