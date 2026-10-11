package store

import (
	"context"
	"time"

	"github.com/jackc/pgx/v5"
)

type Event struct {
	ID          string    `json:"id"`
	Title       string    `json:"title"`
	Description string    `json:"description"`
	Status      string    `json:"status"`
	StartsAt    time.Time `json:"startsAt"`
	EndsAt      time.Time `json:"endsAt"`
}

func (p *Postgres) ListPublishedEvents(ctx context.Context) ([]Event, error) {
	rows, err := p.pool.Query(ctx, `SELECT id, title, description, status, starts_at, ends_at FROM events WHERE status='published' ORDER BY starts_at`)
	if err != nil {
		return nil, err
	}
	return pgx.CollectRows(rows, func(row pgx.CollectableRow) (Event, error) {
		var e Event
		err := row.Scan(&e.ID, &e.Title, &e.Description, &e.Status, &e.StartsAt, &e.EndsAt)
		return e, err
	})
}
