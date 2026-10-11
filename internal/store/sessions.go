package store

import (
	"context"
	"time"
)

func (p *Postgres) CreateSession(ctx context.Context, userID, tokenHash string, expiresAt time.Time) error {
	_, err := p.pool.Exec(ctx, `INSERT INTO sessions (user_id, token_hash, expires_at) VALUES ($1,$2,$3)`, userID, tokenHash, expiresAt)
	return err
}

func (p *Postgres) UserByTokenHash(ctx context.Context, tokenHash string) (User, error) {
	return scanUser(p.pool.QueryRow(ctx,
		`SELECT u.id, u.email, u.display_name, u.role, u.organizer_id, u.password_hash, u.email_verified_at, u.created_at
		 FROM sessions s JOIN users u ON u.id = s.user_id
		 WHERE s.token_hash=$1 AND s.revoked_at IS NULL AND s.expires_at > now()`, tokenHash))
}

func (p *Postgres) RevokeSession(ctx context.Context, tokenHash string) error {
	_, err := p.pool.Exec(ctx, `UPDATE sessions SET revoked_at=now() WHERE token_hash=$1 AND revoked_at IS NULL`, tokenHash)
	return err
}
