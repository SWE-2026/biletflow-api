package store

import (
	"context"
	"errors"
	"time"

	"github.com/jackc/pgx/v5"
)

var ErrInvalidEventState = errors.New("invalid event state")

type Event struct {
	ID                   string     `json:"id"`
	OrganizerID          string     `json:"organizerId"`
	Title                string     `json:"title"`
	Description          string     `json:"description"`
	Category             string     `json:"category"`
	ImageURL             *string    `json:"imageUrl"`
	VenueName            string     `json:"venueName"`
	VenueAddress         string     `json:"venueAddress"`
	Timezone             string     `json:"timezone"`
	Visibility           string     `json:"visibility"`
	Capacity             int        `json:"capacity"`
	Status               string     `json:"status"`
	StartsAt             time.Time  `json:"startsAt"`
	EndsAt               time.Time  `json:"endsAt"`
	RegistrationOpensAt  *time.Time `json:"registrationOpensAt"`
	RegistrationClosesAt *time.Time `json:"registrationClosesAt"`
	CreatedAt            time.Time  `json:"createdAt"`
	UpdatedAt            time.Time  `json:"updatedAt"`
}

type EventInput struct {
	Title                string     `json:"title"`
	Description          string     `json:"description"`
	Category             string     `json:"category"`
	ImageURL             *string    `json:"imageUrl"`
	VenueName            string     `json:"venueName"`
	VenueAddress         string     `json:"venueAddress"`
	Timezone             string     `json:"timezone"`
	Visibility           string     `json:"visibility"`
	Capacity             int        `json:"capacity"`
	StartsAt             time.Time  `json:"startsAt"`
	EndsAt               time.Time  `json:"endsAt"`
	RegistrationOpensAt  *time.Time `json:"registrationOpensAt"`
	RegistrationClosesAt *time.Time `json:"registrationClosesAt"`
}

const eventColumns = `id, organizer_id, title, description, category, image_url, venue_name,
 venue_address, timezone, visibility, capacity, status, starts_at, ends_at,
 registration_opens_at, registration_closes_at, created_at, updated_at`

func scanEvent(row pgx.Row) (Event, error) {
	var e Event
	err := row.Scan(&e.ID, &e.OrganizerID, &e.Title, &e.Description, &e.Category, &e.ImageURL,
		&e.VenueName, &e.VenueAddress, &e.Timezone, &e.Visibility, &e.Capacity, &e.Status,
		&e.StartsAt, &e.EndsAt, &e.RegistrationOpensAt, &e.RegistrationClosesAt,
		&e.CreatedAt, &e.UpdatedAt)
	return e, notFound(err)
}

func (p *Postgres) CreateEvent(ctx context.Context, organizerID string, in EventInput) (Event, error) {
	return scanEvent(p.pool.QueryRow(ctx,
		`INSERT INTO events (organizer_id, title, description, category, image_url, venue_name,
		 venue_address, timezone, visibility, capacity, starts_at, ends_at,
		 registration_opens_at, registration_closes_at)
		 VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14) RETURNING `+eventColumns,
		organizerID, in.Title, in.Description, in.Category, in.ImageURL, in.VenueName,
		in.VenueAddress, in.Timezone, in.Visibility, in.Capacity, in.StartsAt, in.EndsAt,
		in.RegistrationOpensAt, in.RegistrationClosesAt))
}

func (p *Postgres) EventByOwner(ctx context.Context, organizerID, id string) (Event, error) {
	return scanEvent(p.pool.QueryRow(ctx,
		`SELECT `+eventColumns+` FROM events WHERE id=$1 AND organizer_id=$2`, id, organizerID))
}

func (p *Postgres) ListOrganizerEvents(ctx context.Context, organizerID string) ([]Event, error) {
	rows, err := p.pool.Query(ctx,
		`SELECT `+eventColumns+` FROM events WHERE organizer_id=$1 ORDER BY created_at DESC, id`, organizerID)
	if err != nil {
		return nil, err
	}
	return pgx.CollectRows(rows, func(row pgx.CollectableRow) (Event, error) { return scanEvent(row) })
}

func (p *Postgres) ListPublishedEvents(ctx context.Context) ([]Event, error) {
	rows, err := p.pool.Query(ctx,
		`SELECT `+eventColumns+` FROM events WHERE status='published' AND visibility='public' ORDER BY starts_at, id`)
	if err != nil {
		return nil, err
	}
	return pgx.CollectRows(rows, func(row pgx.CollectableRow) (Event, error) { return scanEvent(row) })
}

// PublicEvent returns a published event that attendees may open by link: public or unlisted.
func (p *Postgres) PublicEvent(ctx context.Context, id string) (Event, error) {
	return scanEvent(p.pool.QueryRow(ctx,
		`SELECT `+eventColumns+` FROM events
		 WHERE id=$1 AND status='published' AND visibility IN ('public','unlisted')`, id))
}

// UpdateEvent replaces the event's editable fields. Capacity cannot drop below the event's total
// ticket type quantity.
func (p *Postgres) UpdateEvent(ctx context.Context, organizerID, id string, in EventInput) (Event, error) {
	var e Event
	err := p.withEditableEvent(ctx, organizerID, id, func(tx pgx.Tx) error {
		if err := checkCapacity(ctx, tx, id, "", in.Capacity, 0); err != nil {
			return err
		}
		var err error
		e, err = scanEvent(tx.QueryRow(ctx,
			`UPDATE events SET title=$2, description=$3, category=$4, image_url=$5, venue_name=$6,
			 venue_address=$7, timezone=$8, visibility=$9, capacity=$10, starts_at=$11, ends_at=$12,
			 registration_opens_at=$13, registration_closes_at=$14, updated_at=now()
			 WHERE id=$1 RETURNING `+eventColumns,
			id, in.Title, in.Description, in.Category, in.ImageURL, in.VenueName,
			in.VenueAddress, in.Timezone, in.Visibility, in.Capacity, in.StartsAt, in.EndsAt,
			in.RegistrationOpensAt, in.RegistrationClosesAt))
		return err
	})
	return e, err
}

func (p *Postgres) TransitionEvent(ctx context.Context, organizerID, id, from, to string) (Event, error) {
	e, err := scanEvent(p.pool.QueryRow(ctx,
		`UPDATE events SET status=$4, updated_at=now()
		 WHERE id=$1 AND organizer_id=$2 AND status=$3 RETURNING `+eventColumns,
		id, organizerID, from, to))
	return e, p.eventMutationError(ctx, organizerID, id, err)
}

func (p *Postgres) CancelEvent(ctx context.Context, organizerID, id string) (Event, error) {
	e, err := scanEvent(p.pool.QueryRow(ctx,
		`UPDATE events SET status='cancelled', updated_at=now()
		 WHERE id=$1 AND organizer_id=$2 AND status IN ('draft','published') RETURNING `+eventColumns,
		id, organizerID))
	return e, p.eventMutationError(ctx, organizerID, id, err)
}

func (p *Postgres) eventMutationError(ctx context.Context, organizerID, id string, err error) error {
	if !errors.Is(err, ErrNotFound) {
		return err
	}
	if _, lookupErr := p.EventByOwner(ctx, organizerID, id); lookupErr == nil {
		return ErrInvalidEventState
	} else if !errors.Is(lookupErr, ErrNotFound) {
		return lookupErr
	}
	return ErrNotFound
}
