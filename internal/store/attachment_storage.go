package store

import (
	"context"

	"github.com/jackc/pgx/v5"
)

type objectWriter interface {
	Put(ctx context.Context, key, contentType string, data []byte) error
}

type storedAttachment struct {
	id          string
	caseID      string
	contentType string
	data        []byte
}

func (p *Postgres) MoveAttachmentsToStorage(ctx context.Context, files objectWriter) (int, error) {
	moved := 0
	for {
		batch, err := p.attachmentsInDatabase(ctx)
		if err != nil || len(batch) == 0 {
			return moved, err
		}
		for _, a := range batch {
			key := AttachmentKey(a.caseID, a.id)
			if err := files.Put(ctx, key, a.contentType, a.data); err != nil {
				return moved, err
			}
			if _, err := p.pool.Exec(ctx,
				`UPDATE support_attachments SET storage_key = $2, data = NULL WHERE id = $1 AND storage_key IS NULL`,
				a.id, key); err != nil {
				return moved, err
			}
			moved++
		}
	}
}

func (p *Postgres) attachmentsInDatabase(ctx context.Context) ([]storedAttachment, error) {
	rows, err := p.pool.Query(ctx,
		`SELECT id, case_id, content_type, data FROM support_attachments
		 WHERE storage_key IS NULL ORDER BY id LIMIT 20`)
	if err != nil {
		return nil, err
	}
	return pgx.CollectRows(rows, func(row pgx.CollectableRow) (storedAttachment, error) {
		var a storedAttachment
		err := row.Scan(&a.id, &a.caseID, &a.contentType, &a.data)
		return a, err
	})
}
