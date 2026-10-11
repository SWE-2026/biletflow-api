package store

import (
	"context"
	"time"

	"github.com/jackc/pgx/v5"
)

type User struct {
	ID              string
	Email           string
	DisplayName     string
	Role            string
	OrganizerID     *string
	PasswordHash    *string
	EmailVerifiedAt *time.Time
	CreatedAt       time.Time
}

const userColumns = `id, email, display_name, role, organizer_id, password_hash, email_verified_at, created_at`

func scanUser(row pgx.Row) (User, error) {
	var u User
	err := row.Scan(&u.ID, &u.Email, &u.DisplayName, &u.Role, &u.OrganizerID, &u.PasswordHash, &u.EmailVerifiedAt, &u.CreatedAt)
	return u, notFound(err)
}

// CreateUser inserts an unverified password account. Duplicate emails fail with a unique violation.
func (p *Postgres) CreateUser(ctx context.Context, email, displayName, passwordHash string) (User, error) {
	return scanUser(p.pool.QueryRow(ctx,
		`INSERT INTO users (email, display_name, password_hash) VALUES (lower($1), $2, $3) RETURNING `+userColumns,
		email, displayName, passwordHash))
}

// UpsertVerifiedUser returns the account for email, creating it when missing, and marks the email
// verified. Used after the owner has proven control of the inbox with a one-time code.
func (p *Postgres) UpsertVerifiedUser(ctx context.Context, email string) (User, error) {
	return scanUser(p.pool.QueryRow(ctx,
		`INSERT INTO users (email, email_verified_at) VALUES (lower($1), now())
		 ON CONFLICT (email) DO UPDATE SET email_verified_at = COALESCE(users.email_verified_at, now())
		 RETURNING `+userColumns, email))
}

func (p *Postgres) UserByEmail(ctx context.Context, email string) (User, error) {
	return scanUser(p.pool.QueryRow(ctx, `SELECT `+userColumns+` FROM users WHERE email=lower($1)`, email))
}

func (p *Postgres) UserByID(ctx context.Context, id string) (User, error) {
	return scanUser(p.pool.QueryRow(ctx, `SELECT `+userColumns+` FROM users WHERE id=$1`, id))
}
