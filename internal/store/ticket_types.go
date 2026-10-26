package store

import (
	"context"
	"errors"
	"time"

	"github.com/jackc/pgx/v5"
)

var (
	ErrCapacityExceeded = errors.New("ticket quantities exceed event capacity")
	ErrTicketTypeInUse  = errors.New("ticket type has orders")
)

type TicketType struct {
	ID            string     `json:"id"`
	EventID       string     `json:"eventId"`
	Name          string     `json:"name"`
	Description   string     `json:"description"`
	PriceAmount   int64      `json:"priceAmount"`
	Quantity      int        `json:"quantity"`
	SalesStartsAt *time.Time `json:"salesStartsAt"`
	SalesEndsAt   *time.Time `json:"salesEndsAt"`
	MaxPerOrder   int        `json:"maxPerOrder"`
	Hidden        bool       `json:"hidden"`
	SortOrder     int        `json:"sortOrder"`
	CreatedAt     time.Time  `json:"createdAt"`
	UpdatedAt     time.Time  `json:"updatedAt"`
}

type TicketTypeInput struct {
	Name          string     `json:"name"`
	Description   string     `json:"description"`
	PriceAmount   int64      `json:"priceAmount"`
	Quantity      int        `json:"quantity"`
	SalesStartsAt *time.Time `json:"salesStartsAt"`
	SalesEndsAt   *time.Time `json:"salesEndsAt"`
	MaxPerOrder   int        `json:"maxPerOrder"`
	Hidden        bool       `json:"hidden"`
	SortOrder     int        `json:"sortOrder"`
}

const ticketTypeColumns = `id, event_id, name, description, price_amount, quantity, sales_starts_at,
 sales_ends_at, max_per_order, hidden, sort_order, created_at, updated_at`

func scanTicketType(row pgx.Row) (TicketType, error) {
	var t TicketType
	err := row.Scan(&t.ID, &t.EventID, &t.Name, &t.Description, &t.PriceAmount, &t.Quantity,
		&t.SalesStartsAt, &t.SalesEndsAt, &t.MaxPerOrder, &t.Hidden, &t.SortOrder,
		&t.CreatedAt, &t.UpdatedAt)
	return t, notFound(err)
}

func collectTicketTypes(rows pgx.Rows, err error) ([]TicketType, error) {
	if err != nil {
		return nil, err
	}
	return pgx.CollectRows(rows, func(row pgx.CollectableRow) (TicketType, error) { return scanTicketType(row) })
}

// withEditableEvent runs fn in a transaction that holds a row lock on the organizer's event, so
// capacity checks and ticket type changes for one event are serialized. Cancelled events are read-only.
func (p *Postgres) withEditableEvent(ctx context.Context, organizerID, eventID string, fn func(tx pgx.Tx) error) error {
	return pgx.BeginFunc(ctx, p.pool, func(tx pgx.Tx) error {
		var status string
		err := tx.QueryRow(ctx,
			`SELECT status FROM events WHERE id=$1 AND organizer_id=$2 FOR UPDATE`, eventID, organizerID).Scan(&status)
		if err != nil {
			return notFound(err)
		}
		if status == "cancelled" {
			return ErrInvalidEventState
		}
		return fn(tx)
	})
}

// checkCapacity fails when the event's ticket quantities, with excludeTypeID's quantity replaced by
// extra, would exceed capacity. Call it while holding the event row lock.
func checkCapacity(ctx context.Context, tx pgx.Tx, eventID, excludeTypeID string, capacity, extra int) error {
	var total int64
	err := tx.QueryRow(ctx,
		`SELECT COALESCE(SUM(quantity), 0) FROM ticket_types WHERE event_id=$1 AND id::text <> $2`,
		eventID, excludeTypeID).Scan(&total)
	if err != nil {
		return err
	}
	if total+int64(extra) > int64(capacity) {
		return ErrCapacityExceeded
	}
	return nil
}

func eventCapacity(ctx context.Context, tx pgx.Tx, eventID string) (int, error) {
	var capacity int
	err := tx.QueryRow(ctx, `SELECT capacity FROM events WHERE id=$1`, eventID).Scan(&capacity)
	return capacity, err
}

func (p *Postgres) ListTicketTypes(ctx context.Context, organizerID, eventID string) ([]TicketType, error) {
	if _, err := p.EventByOwner(ctx, organizerID, eventID); err != nil {
		return nil, err
	}
	return collectTicketTypes(p.pool.Query(ctx,
		`SELECT `+ticketTypeColumns+` FROM ticket_types WHERE event_id=$1 ORDER BY sort_order, created_at, id`, eventID))
}

func (p *Postgres) CreateTicketType(ctx context.Context, organizerID, eventID string, in TicketTypeInput) (TicketType, error) {
	var t TicketType
	err := p.withEditableEvent(ctx, organizerID, eventID, func(tx pgx.Tx) error {
		capacity, err := eventCapacity(ctx, tx, eventID)
		if err != nil {
			return err
		}
		if err := checkCapacity(ctx, tx, eventID, "", capacity, in.Quantity); err != nil {
			return err
		}
		t, err = scanTicketType(tx.QueryRow(ctx,
			`INSERT INTO ticket_types (event_id, name, description, price_amount, quantity,
			 sales_starts_at, sales_ends_at, max_per_order, hidden, sort_order)
			 VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10) RETURNING `+ticketTypeColumns,
			eventID, in.Name, in.Description, in.PriceAmount, in.Quantity,
			in.SalesStartsAt, in.SalesEndsAt, in.MaxPerOrder, in.Hidden, in.SortOrder))
		return err
	})
	return t, err
}

func (p *Postgres) UpdateTicketType(ctx context.Context, organizerID, eventID, id string, in TicketTypeInput) (TicketType, error) {
	var t TicketType
	err := p.withEditableEvent(ctx, organizerID, eventID, func(tx pgx.Tx) error {
		capacity, err := eventCapacity(ctx, tx, eventID)
		if err != nil {
			return err
		}
		if err := checkCapacity(ctx, tx, eventID, id, capacity, in.Quantity); err != nil {
			return err
		}
		t, err = scanTicketType(tx.QueryRow(ctx,
			`UPDATE ticket_types SET name=$3, description=$4, price_amount=$5, quantity=$6,
			 sales_starts_at=$7, sales_ends_at=$8, max_per_order=$9, hidden=$10, sort_order=$11,
			 updated_at=now()
			 WHERE id=$1 AND event_id=$2 RETURNING `+ticketTypeColumns,
			id, eventID, in.Name, in.Description, in.PriceAmount, in.Quantity,
			in.SalesStartsAt, in.SalesEndsAt, in.MaxPerOrder, in.Hidden, in.SortOrder))
		return err
	})
	return t, err
}

func (p *Postgres) DeleteTicketType(ctx context.Context, organizerID, eventID, id string) error {
	return p.withEditableEvent(ctx, organizerID, eventID, func(tx pgx.Tx) error {
		var ordered bool
		if err := tx.QueryRow(ctx,
			`SELECT EXISTS(SELECT 1 FROM order_items WHERE ticket_type_id=$1)`, id).Scan(&ordered); err != nil {
			return err
		}
		if ordered {
			return ErrTicketTypeInUse
		}
		tag, err := tx.Exec(ctx, `DELETE FROM ticket_types WHERE id=$1 AND event_id=$2`, id, eventID)
		if err == nil && tag.RowsAffected() == 0 {
			return ErrNotFound
		}
		return err
	})
}
