package store

import (
	"context"

	"github.com/jackc/pgx/v5"
)

func (p *Postgres) DuplicateEvent(ctx context.Context, organizerID, actorID, sourceID string) (Event, error) {
	var e Event
	err := pgx.BeginFunc(ctx, p.pool, func(tx pgx.Tx) error {
		var err error
		e, err = scanEvent(tx.QueryRow(ctx,
			`INSERT INTO events (organizer_id, title, description, category, image_url, venue_name,
			 venue_address, timezone, visibility, capacity, starts_at, ends_at, refund_policy)
			 SELECT organizer_id, title, description, category, image_url, venue_name,
			 venue_address, timezone, visibility, capacity, starts_at, ends_at, refund_policy
			 FROM events WHERE id = $1 AND organizer_id = $2 RETURNING `+eventColumns, sourceID, organizerID))
		if err != nil {
			return err
		}
		if _, err := tx.Exec(ctx,
			`INSERT INTO ticket_types (event_id, name, description, price_amount, quantity,
			 max_per_order, hidden, sort_order)
			 SELECT $2, name, description, price_amount, quantity, max_per_order, hidden, sort_order
			 FROM ticket_types WHERE event_id = $1 ORDER BY sort_order, created_at, id`, sourceID, e.ID); err != nil {
			return err
		}
		return writeAudit(ctx, tx, auditEntry{eventID: e.ID, actorID: actorID, action: "event.duplicated",
			entityType: "event", entityID: e.ID, details: map[string]any{"sourceEventId": sourceID}})
	})
	return e, err
}
