package store

import (
	"context"

	"github.com/jackc/pgx/v5"
)

type auditEntry struct {
	eventID    string
	actorID    string
	action     string
	entityType string
	entityID   string
	details    map[string]any
}

func writeAudit(ctx context.Context, tx pgx.Tx, a auditEntry) error {
	_, err := tx.Exec(ctx,
		`INSERT INTO audit_log (event_id, actor_id, action, entity_type, entity_id, details)
		 VALUES (NULLIF($1, '')::uuid, NULLIF($2, '')::uuid, $3, $4, NULLIF($5, '')::uuid, $6)`,
		a.eventID, a.actorID, a.action, a.entityType, a.entityID, a.details)
	return err
}
