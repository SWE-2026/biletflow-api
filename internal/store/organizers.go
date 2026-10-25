package store

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
)

var (
	ErrEmailUnverified = errors.New("email not verified")
	ErrRoleConflict    = errors.New("account cannot become an organizer")
)

type Organizer struct {
	ID                     string    `json:"id"`
	Type                   string    `json:"type"`
	Name                   string    `json:"name"`
	ContactEmail           string    `json:"contactEmail"`
	ContactPhone           string    `json:"contactPhone"`
	PayoutAccountReference string    `json:"payoutAccountReference"`
	CreatedAt              time.Time `json:"createdAt"`
}

type OrganizerInput struct {
	Type                   string `json:"type"`
	Name                   string `json:"name"`
	ContactEmail           string `json:"contactEmail"`
	ContactPhone           string `json:"contactPhone"`
	PayoutAccountReference string `json:"payoutAccountReference"`
}

const organizerColumns = `id, type, name, contact_email, contact_phone, payout_account_reference, created_at`

func scanOrganizer(row pgx.Row) (Organizer, error) {
	var o Organizer
	err := row.Scan(&o.ID, &o.Type, &o.Name, &o.ContactEmail, &o.ContactPhone, &o.PayoutAccountReference, &o.CreatedAt)
	return o, notFound(err)
}

func (p *Postgres) CreateOrganizer(ctx context.Context, userID string, in OrganizerInput) (Organizer, User, error) {
	var o Organizer
	err := pgx.BeginFunc(ctx, p.pool, func(tx pgx.Tx) error {
		var role string
		var verifiedAt *time.Time
		if err := tx.QueryRow(ctx, `SELECT role, email_verified_at FROM users WHERE id=$1 FOR UPDATE`, userID).
			Scan(&role, &verifiedAt); err != nil {
			return notFound(err)
		}
		if role != "customer" {
			return ErrRoleConflict
		}
		if verifiedAt == nil {
			return ErrEmailUnverified
		}
		var err error
		o, err = scanOrganizer(tx.QueryRow(ctx,
			`INSERT INTO organizers (type, name, contact_email, contact_phone, payout_account_reference)
			 VALUES ($1,$2,$3,$4,$5) RETURNING `+organizerColumns,
			in.Type, in.Name, in.ContactEmail, in.ContactPhone, in.PayoutAccountReference))
		if err != nil {
			return fmt.Errorf("create organizer: %w", err)
		}
		_, err = tx.Exec(ctx, `UPDATE users SET role='organizer', organizer_id=$1 WHERE id=$2`, o.ID, userID)
		return err
	})
	if err != nil {
		return Organizer{}, User{}, err
	}
	u, err := p.UserByID(ctx, userID)
	return o, u, err
}

func (p *Postgres) OrganizerByID(ctx context.Context, id string) (Organizer, error) {
	return scanOrganizer(p.pool.QueryRow(ctx, `SELECT `+organizerColumns+` FROM organizers WHERE id=$1`, id))
}

func (p *Postgres) UpdateOrganizer(ctx context.Context, id string, in OrganizerInput) (Organizer, error) {
	return scanOrganizer(p.pool.QueryRow(ctx,
		`UPDATE organizers SET type=$2, name=$3, contact_email=$4, contact_phone=$5,
		 payout_account_reference=$6 WHERE id=$1 RETURNING `+organizerColumns,
		id, in.Type, in.Name, in.ContactEmail, in.ContactPhone, in.PayoutAccountReference))
}
