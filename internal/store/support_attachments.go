package store

import (
	"context"

	"github.com/jackc/pgx/v5"
)

type SupportFile struct {
	Name        string
	ContentType string
	Data        []byte
	Key         string
}

func AttachmentKey(caseID, name string) string {
	return "support/" + caseID + "/" + name
}

type SupportAttachment struct {
	ID          string `json:"id"`
	FileName    string `json:"fileName"`
	ContentType string `json:"contentType"`
	SizeBytes   int    `json:"sizeBytes"`
}

func insertAttachments(ctx context.Context, tx pgx.Tx, caseID, messageID string, files []SupportFile) error {
	for _, f := range files {
		if _, err := tx.Exec(ctx,
			`INSERT INTO support_attachments (case_id, message_id, file_name, content_type, size_bytes, storage_key)
			 VALUES ($1, $2, $3, $4, $5, $6)`, caseID, messageID, f.Name, f.ContentType, len(f.Data), f.Key); err != nil {
			return err
		}
	}
	return nil
}

type attachmentRow struct {
	messageID  string
	attachment SupportAttachment
}

func attachSupportFiles(ctx context.Context, q querier, caseID string, messages []SupportMessage) error {
	rows, err := q.Query(ctx,
		`SELECT message_id, id, file_name, content_type, size_bytes FROM support_attachments
		 WHERE case_id = $1 ORDER BY created_at, id`, caseID)
	if err != nil {
		return err
	}
	list, err := pgx.CollectRows(rows, func(row pgx.CollectableRow) (attachmentRow, error) {
		var r attachmentRow
		a := &r.attachment
		err := row.Scan(&r.messageID, &a.ID, &a.FileName, &a.ContentType, &a.SizeBytes)
		return r, err
	})
	if err != nil {
		return err
	}
	index := make(map[string]int, len(messages))
	for i := range messages {
		messages[i].Attachments = []SupportAttachment{}
		index[messages[i].ID] = i
	}
	for _, r := range list {
		if i, ok := index[r.messageID]; ok {
			messages[i].Attachments = append(messages[i].Attachments, r.attachment)
		}
	}
	return nil
}

func (p *Postgres) SupportAttachmentFile(ctx context.Context, v SupportViewer, caseID, attachmentID string) (SupportAttachment, string, error) {
	var a SupportAttachment
	var key string
	err := p.pool.QueryRow(ctx,
		`SELECT a.id, a.file_name, a.content_type, a.size_bytes, a.storage_key
		 FROM support_attachments a JOIN support_cases c ON c.id = a.case_id
		 WHERE c.id = $4 AND a.id = $5 AND a.storage_key IS NOT NULL AND `+caseAccess, append(v.args(), caseID, attachmentID)...).
		Scan(&a.ID, &a.FileName, &a.ContentType, &a.SizeBytes, &key)
	return a, key, notFound(err)
}
