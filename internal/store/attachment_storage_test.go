package store

import (
	"bytes"
	"context"
	"crypto/rand"
	"os"
	"testing"

	"github.com/SWE-2026/biletflow-api/internal/storage"
)

func TestMoveAttachmentsToStorage(t *testing.T) {
	dsn := os.Getenv("TEST_DB_DSN")
	if dsn == "" {
		t.Skip("TEST_DB_DSN not set; run `make test` for DB-backed tests")
	}
	ctx := context.Background()
	db, err := NewPostgres(ctx, dsn)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(db.Close)
	if err := db.Migrate(ctx); err != nil {
		t.Fatal(err)
	}
	data := []byte("legacy attachment " + rand.Text())
	var caseID, attachmentID string
	if err := db.pool.QueryRow(ctx, `
		WITH o AS (INSERT INTO organizers (type) VALUES ('individual') RETURNING id),
		u AS (INSERT INTO users (email, display_name, password_hash)
		      VALUES ($1, 'Legacy', '') RETURNING id),
		c AS (INSERT INTO support_cases (kind, requester_id, organizer_id, category, subject, last_activity_by)
		      SELECT 'attendee', u.id, o.id, 'technical', 'Legacy', u.id FROM u, o RETURNING id),
		m AS (INSERT INTO support_messages (case_id, kind, author_id, body)
		      SELECT c.id, 'message', u.id, '' FROM c, u RETURNING id, case_id)
		INSERT INTO support_attachments (case_id, message_id, file_name, content_type, size_bytes, data)
		SELECT m.case_id, m.id, 'old.txt', 'text/plain', $2, $3 FROM m RETURNING case_id, id`,
		"legacy-"+rand.Text()+"@test.biletflow.kz", len(data), data).Scan(&caseID, &attachmentID); err != nil {
		t.Fatal(err)
	}

	files := storage.NewMemory()
	if _, err := db.MoveAttachmentsToStorage(ctx, files); err != nil {
		t.Fatal(err)
	}
	key := AttachmentKey(caseID, attachmentID)
	got, err := files.Get(ctx, key)
	if err != nil || !bytes.Equal(got, data) {
		t.Fatalf("object %s = %q, %v; want %q", key, got, err, data)
	}
	var storedKey string
	var dataCleared bool
	if err := db.pool.QueryRow(ctx,
		`SELECT storage_key, data IS NULL FROM support_attachments WHERE id = $1`, attachmentID).
		Scan(&storedKey, &dataCleared); err != nil {
		t.Fatal(err)
	}
	if storedKey != key || !dataCleared {
		t.Fatalf("row not updated: key %q, data cleared %v", storedKey, dataCleared)
	}
	if moved, err := db.MoveAttachmentsToStorage(ctx, files); err != nil || moved != 0 {
		t.Fatalf("second run moved %d, %v; want 0", moved, err)
	}
}
