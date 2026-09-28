package store

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

type Postgres struct{ pool *pgxpool.Pool }

func NewPostgres(ctx context.Context, url string) (*Postgres, error) {
	p, err := pgxpool.New(ctx, url)
	if err != nil {
		return nil, fmt.Errorf("connect database: %w", err)
	}
	if err := p.Ping(ctx); err != nil {
		p.Close()
		return nil, fmt.Errorf("ping database: %w", err)
	}
	return &Postgres{pool: p}, nil
}
func (p *Postgres) Close()                         { p.pool.Close() }
func (p *Postgres) Ping(ctx context.Context) error { return p.pool.Ping(ctx) }

func (p *Postgres) Migrate(ctx context.Context) error {
	if _, err := p.pool.Exec(ctx, `CREATE TABLE IF NOT EXISTS schema_migrations (name TEXT PRIMARY KEY, applied_at TIMESTAMPTZ NOT NULL DEFAULT now())`); err != nil {
		return err
	}
	entries, err := os.ReadDir("migrations")
	if err != nil {
		return err
	}
	sort.Slice(entries, func(i, j int) bool { return entries[i].Name() < entries[j].Name() })
	for _, entry := range entries {
		name := entry.Name()
		var done bool
		if err := p.pool.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM schema_migrations WHERE name=$1)`, name).Scan(&done); err != nil {
			return err
		}
		if done {
			continue
		}
		sql, err := os.ReadFile(filepath.Join("migrations", name))
		if err != nil {
			return err
		}
		tx, err := p.pool.Begin(ctx)
		if err != nil {
			return err
		}
		_, execErr := tx.Exec(ctx, string(sql))
		if execErr == nil {
			_, execErr = tx.Exec(ctx, `INSERT INTO schema_migrations (name) VALUES ($1)`, name)
		}
		if execErr != nil {
			_ = tx.Rollback(ctx)
			return fmt.Errorf("migration %s: %w", name, execErr)
		}
		if err := tx.Commit(ctx); err != nil {
			return err
		}
	}
	return nil
}

type User struct {
	ID, Email, DisplayName, Role, PasswordHash string
	CreatedAt                                  time.Time
}
type Event struct {
	ID          string    `json:"id"`
	Title       string    `json:"title"`
	Description string    `json:"description"`
	Status      string    `json:"status"`
	StartsAt    time.Time `json:"startsAt"`
	EndsAt      time.Time `json:"endsAt"`
}

func (p *Postgres) CreateUser(ctx context.Context, email, displayName, passwordHash string) (User, error) {
	var u User
	err := p.pool.QueryRow(ctx, `INSERT INTO users (email, display_name, password_hash) VALUES (lower($1), $2, $3) RETURNING id, email, display_name, role, created_at`, email, displayName, passwordHash).Scan(&u.ID, &u.Email, &u.DisplayName, &u.Role, &u.CreatedAt)
	return u, err
}
func (p *Postgres) UserByEmail(ctx context.Context, email string) (User, error) {
	var u User
	err := p.pool.QueryRow(ctx, `SELECT id, email, display_name, role, password_hash, created_at FROM users WHERE email=lower($1)`, email).Scan(&u.ID, &u.Email, &u.DisplayName, &u.Role, &u.PasswordHash, &u.CreatedAt)
	return u, err
}
func (p *Postgres) CreateSession(ctx context.Context, userID, hash string, expiresAt time.Time) error {
	_, err := p.pool.Exec(ctx, `INSERT INTO sessions (user_id, token_hash, expires_at) VALUES ($1,$2,$3)`, userID, hash, expiresAt)
	return err
}
func (p *Postgres) UserByTokenHash(ctx context.Context, hash string) (User, error) {
	var u User
	err := p.pool.QueryRow(ctx, `SELECT u.id,u.email,u.display_name,u.role,u.created_at FROM sessions s JOIN users u ON u.id=s.user_id WHERE s.token_hash=$1 AND s.revoked_at IS NULL AND s.expires_at > now()`, hash).Scan(&u.ID, &u.Email, &u.DisplayName, &u.Role, &u.CreatedAt)
	return u, err
}
func (p *Postgres) RevokeSession(ctx context.Context, hash string) error {
	_, err := p.pool.Exec(ctx, `UPDATE sessions SET revoked_at=now() WHERE token_hash=$1 AND revoked_at IS NULL`, hash)
	return err
}
func (p *Postgres) ListPublishedEvents(ctx context.Context) ([]Event, error) {
	rows, err := p.pool.Query(ctx, `SELECT id,title,description,status,starts_at,ends_at FROM events WHERE status='published' ORDER BY starts_at`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var result []Event
	for rows.Next() {
		var e Event
		if err := rows.Scan(&e.ID, &e.Title, &e.Description, &e.Status, &e.StartsAt, &e.EndsAt); err != nil {
			return nil, err
		}
		result = append(result, e)
	}
	return result, rows.Err()
}
func IsUniqueViolation(err error) bool { return strings.Contains(err.Error(), "duplicate key") }
