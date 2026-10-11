package store

import (
	"context"
	"errors"
	"time"
)

// ErrAttemptsExhausted is returned when a code exists but has no verification attempts left.
var ErrAttemptsExhausted = errors.New("code attempts exhausted")

type EmailCode struct {
	Email     string
	Purpose   string
	CodeHash  string
	Attempts  int
	ExpiresAt time.Time
	ResendAt  time.Time
}

func (p *Postgres) GetEmailCode(ctx context.Context, email, purpose string) (EmailCode, error) {
	c := EmailCode{Email: email, Purpose: purpose}
	err := p.pool.QueryRow(ctx,
		`SELECT code_hash, attempts, expires_at, resend_at FROM email_codes WHERE email=lower($1) AND purpose=$2`,
		email, purpose).Scan(&c.CodeHash, &c.Attempts, &c.ExpiresAt, &c.ResendAt)
	return c, notFound(err)
}

// PutEmailCode stores a fresh code, replacing any earlier one for the same email and purpose.
func (p *Postgres) PutEmailCode(ctx context.Context, c EmailCode) error {
	_, err := p.pool.Exec(ctx,
		`INSERT INTO email_codes (email, purpose, code_hash, expires_at, resend_at) VALUES (lower($1), $2, $3, $4, $5)
		 ON CONFLICT (email, purpose) DO UPDATE
		 SET code_hash=EXCLUDED.code_hash, attempts=0, expires_at=EXCLUDED.expires_at, resend_at=EXCLUDED.resend_at, created_at=now()`,
		c.Email, c.Purpose, c.CodeHash, c.ExpiresAt, c.ResendAt)
	return err
}

// ConsumeCodeAttempt atomically spends one verification attempt and returns the stored code, so
// concurrent guesses can never exceed maxAttempts.
func (p *Postgres) ConsumeCodeAttempt(ctx context.Context, email, purpose string, maxAttempts int) (EmailCode, error) {
	c := EmailCode{Email: email, Purpose: purpose}
	err := p.pool.QueryRow(ctx,
		`UPDATE email_codes SET attempts = attempts + 1
		 WHERE email=lower($1) AND purpose=$2 AND attempts < $3
		 RETURNING code_hash, attempts, expires_at, resend_at`,
		email, purpose, maxAttempts).Scan(&c.CodeHash, &c.Attempts, &c.ExpiresAt, &c.ResendAt)
	if err = notFound(err); !errors.Is(err, ErrNotFound) {
		return c, err
	}
	if _, getErr := p.GetEmailCode(ctx, email, purpose); getErr == nil {
		return c, ErrAttemptsExhausted
	}
	return c, ErrNotFound
}

func (p *Postgres) DeleteEmailCode(ctx context.Context, email, purpose string) error {
	_, err := p.pool.Exec(ctx, `DELETE FROM email_codes WHERE email=lower($1) AND purpose=$2`, email, purpose)
	return err
}
