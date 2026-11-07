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
	ImageURLs            []string   `json:"imageUrls"`
	VenueName            string     `json:"venueName"`
	VenueAddress         string     `json:"venueAddress"`
	Timezone             string     `json:"timezone"`
	Visibility           string     `json:"visibility"`
	Capacity             int        `json:"capacity"`
	Status               string     `json:"status"`
	Phase                string     `json:"phase"`
	StartsAt             time.Time  `json:"startsAt"`
	EndsAt               time.Time  `json:"endsAt"`
	RegistrationOpensAt  *time.Time `json:"registrationOpensAt"`
	RegistrationClosesAt *time.Time `json:"registrationClosesAt"`
	RefundPolicy         string     `json:"refundPolicy"`
	CreatedAt            time.Time  `json:"createdAt"`
	UpdatedAt            time.Time  `json:"updatedAt"`
}

type EventInput struct {
	Title                string     `json:"title"`
	Description          string     `json:"description"`
	Category             string     `json:"category"`
	ImageURL             *string    `json:"imageUrl"`
	ImageURLs            []string   `json:"imageUrls"`
	VenueName            string     `json:"venueName"`
	VenueAddress         string     `json:"venueAddress"`
	Timezone             string     `json:"timezone"`
	Visibility           string     `json:"visibility"`
	Capacity             int        `json:"capacity"`
	StartsAt             time.Time  `json:"startsAt"`
	EndsAt               time.Time  `json:"endsAt"`
	RegistrationOpensAt  *time.Time `json:"registrationOpensAt"`
	RegistrationClosesAt *time.Time `json:"registrationClosesAt"`
	RefundPolicy         string     `json:"refundPolicy"`
}

const eventColumns = `id, organizer_id, title, description, category, image_url, image_urls, venue_name,
 venue_address, timezone, visibility, capacity, status, starts_at, ends_at,
 registration_opens_at, registration_closes_at, refund_policy, created_at, updated_at`

func scanEvent(row pgx.Row) (Event, error) {
	var e Event
	err := row.Scan(&e.ID, &e.OrganizerID, &e.Title, &e.Description, &e.Category, &e.ImageURL, &e.ImageURLs,
		&e.VenueName, &e.VenueAddress, &e.Timezone, &e.Visibility, &e.Capacity, &e.Status,
		&e.StartsAt, &e.EndsAt, &e.RegistrationOpensAt, &e.RegistrationClosesAt,
		&e.RefundPolicy, &e.CreatedAt, &e.UpdatedAt)
	e.Phase = eventPhase(e, time.Now())
	return e, notFound(err)
}

func eventPhase(e Event, now time.Time) string {
	switch {
	case e.Status == "cancelled":
		return "cancelled"
	case now.Before(e.StartsAt):
		return "upcoming"
	case now.Before(e.EndsAt):
		return "active"
	default:
		return "completed"
	}
}

func (p *Postgres) CreateEvent(ctx context.Context, organizerID, actorID string, in EventInput) (Event, error) {
	var e Event
	err := pgx.BeginFunc(ctx, p.pool, func(tx pgx.Tx) error {
		var err error
		if e, err = insertEvent(ctx, tx, organizerID, in); err != nil {
			return err
		}
		return writeAudit(ctx, tx, auditEntry{eventID: e.ID, actorID: actorID, action: "event.created",
			entityType: "event", entityID: e.ID, details: map[string]any{"title": e.Title}})
	})
	return e, err
}

func insertEvent(ctx context.Context, tx pgx.Tx, organizerID string, in EventInput) (Event, error) {
	return scanEvent(tx.QueryRow(ctx,
		`INSERT INTO events (organizer_id, title, description, category, image_url, venue_name,
		 venue_address, timezone, visibility, capacity, starts_at, ends_at,
		 registration_opens_at, registration_closes_at, refund_policy, image_urls)
		 VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14,$15,$16) RETURNING `+eventColumns,
		organizerID, in.Title, in.Description, in.Category, in.ImageURL, in.VenueName,
		in.VenueAddress, in.Timezone, in.Visibility, in.Capacity, in.StartsAt, in.EndsAt,
		in.RegistrationOpensAt, in.RegistrationClosesAt, in.RefundPolicy, in.ImageURLs))
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
func (p *Postgres) UpdateEvent(ctx context.Context, organizerID, actorID, id string, in EventInput) (Event, error) {
	var e Event
	err := p.withEditableEvent(ctx, organizerID, id, func(tx pgx.Tx) error {
		if err := checkCapacity(ctx, tx, id, "", in.Capacity, 0); err != nil {
			return err
		}
		before, err := scanEvent(tx.QueryRow(ctx, `SELECT `+eventColumns+` FROM events WHERE id=$1`, id))
		if err != nil {
			return err
		}
		details := map[string]any{}
		if before.Capacity != in.Capacity {
			details["capacity"] = []int{before.Capacity, in.Capacity}
		}
		if err := writeAudit(ctx, tx, auditEntry{eventID: id, actorID: actorID, action: "event.updated",
			entityType: "event", entityID: id, details: details}); err != nil {
			return err
		}
		if attendeeFacingChange(before, in) {
			if err := notifyTicketHolders(ctx, tx, id, notice{kind: "event_updated", link: "/events/" + id,
				title: "Event updated: " + in.Title,
				body:  "The organizer changed the date, time or venue of " + in.Title + ". Check the event page for the new details; your tickets stay valid."}); err != nil {
				return err
			}
		}
		e, err = scanEvent(tx.QueryRow(ctx,
			`UPDATE events SET title=$2, description=$3, category=$4, image_url=$5, venue_name=$6,
			 venue_address=$7, timezone=$8, visibility=$9, capacity=$10, starts_at=$11, ends_at=$12,
			 registration_opens_at=$13, registration_closes_at=$14, refund_policy=$15, image_urls=$16,
			 updated_at=now()
			 WHERE id=$1 RETURNING `+eventColumns,
			id, in.Title, in.Description, in.Category, in.ImageURL, in.VenueName,
			in.VenueAddress, in.Timezone, in.Visibility, in.Capacity, in.StartsAt, in.EndsAt,
			in.RegistrationOpensAt, in.RegistrationClosesAt, in.RefundPolicy, in.ImageURLs))
		return err
	})
	return e, err
}

func attendeeFacingChange(before Event, in EventInput) bool {
	return before.Status == "published" && (!before.StartsAt.Equal(in.StartsAt) || !before.EndsAt.Equal(in.EndsAt) ||
		before.VenueName != in.VenueName || before.VenueAddress != in.VenueAddress || before.Title != in.Title)
}

func (p *Postgres) TransitionEvent(ctx context.Context, organizerID, actorID, id, from, to string) (Event, error) {
	action := "event.published"
	if to == "draft" {
		action = "event.unpublished"
	}
	return p.changeEventStatus(ctx, organizerID, actorID, id, []string{from}, to, action)
}

func (p *Postgres) CancelEvent(ctx context.Context, organizerID, actorID, id string) (Event, error) {
	return p.changeEventStatus(ctx, organizerID, actorID, id, []string{"draft", "published", "suspended"}, "cancelled", "event.cancelled")
}

func (p *Postgres) changeEventStatus(ctx context.Context, organizerID, actorID, id string, from []string, to, action string) (Event, error) {
	var e Event
	err := pgx.BeginFunc(ctx, p.pool, func(tx pgx.Tx) error {
		var err error
		e, err = scanEvent(tx.QueryRow(ctx,
			`UPDATE events SET status=$4, updated_at=now()
			 WHERE id=$1 AND organizer_id=$2 AND status = ANY($3::text[]) RETURNING `+eventColumns,
			id, organizerID, from, to))
		if err != nil {
			return err
		}
		if to == "cancelled" {
			if err := notifyTicketHolders(ctx, tx, id, notice{kind: "event_cancelled", link: "/events/" + id,
				title: "Event cancelled: " + e.Title,
				body:  e.Title + " has been cancelled by the organizer. Check your order for refund details or open a support case."}); err != nil {
				return err
			}
		}
		return writeAudit(ctx, tx, auditEntry{eventID: id, actorID: actorID, action: action,
			entityType: "event", entityID: id, details: map[string]any{"status": to}})
	})
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
