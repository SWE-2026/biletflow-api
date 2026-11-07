package store

import (
	"context"
	"errors"
	"time"

	"github.com/jackc/pgx/v5"
)

var ErrUserNotFound = errors.New("no account with this email")

type StaffMember struct {
	UserID      string    `json:"userId"`
	Email       string    `json:"email"`
	DisplayName string    `json:"displayName"`
	Role        string    `json:"role"`
	CreatedAt   time.Time `json:"createdAt"`
}

const staffSelect = `SELECT u.id, u.email, u.display_name, s.role, s.created_at
 FROM event_staff s JOIN users u ON u.id = s.user_id`

func scanStaff(row pgx.Row) (StaffMember, error) {
	var m StaffMember
	err := row.Scan(&m.UserID, &m.Email, &m.DisplayName, &m.Role, &m.CreatedAt)
	return m, notFound(err)
}

func (p *Postgres) ListStaff(ctx context.Context, organizerID, eventID string) ([]StaffMember, error) {
	if _, err := p.EventByOwner(ctx, organizerID, eventID); err != nil {
		return nil, err
	}
	rows, err := p.pool.Query(ctx, staffSelect+` WHERE s.event_id = $1 ORDER BY s.created_at, u.email`, eventID)
	if err != nil {
		return nil, err
	}
	return pgx.CollectRows(rows, func(row pgx.CollectableRow) (StaffMember, error) { return scanStaff(row) })
}

func (p *Postgres) AssignStaff(ctx context.Context, organizerID, actorID, eventID, email, role string) (StaffMember, error) {
	var m StaffMember
	err := p.withEditableEvent(ctx, organizerID, eventID, func(tx pgx.Tx) error {
		var userID string
		if err := tx.QueryRow(ctx, `SELECT id FROM users WHERE email = lower($1)`, email).Scan(&userID); err != nil {
			if errors.Is(err, pgx.ErrNoRows) {
				return ErrUserNotFound
			}
			return err
		}
		if _, err := tx.Exec(ctx,
			`INSERT INTO event_staff (event_id, user_id, role, added_by) VALUES ($1, $2, $3, $4)
			 ON CONFLICT (event_id, user_id) DO UPDATE SET role = EXCLUDED.role`,
			eventID, userID, role, actorID); err != nil {
			return err
		}
		var err error
		m, err = scanStaff(tx.QueryRow(ctx, staffSelect+` WHERE s.event_id = $1 AND s.user_id = $2`, eventID, userID))
		if err != nil {
			return err
		}
		return writeAudit(ctx, tx, auditEntry{eventID: eventID, actorID: actorID, action: "staff.assigned",
			entityType: "user", entityID: userID, details: map[string]any{"role": role}})
	})
	return m, err
}

func (p *Postgres) RemoveStaff(ctx context.Context, organizerID, actorID, eventID, userID string) error {
	return pgx.BeginFunc(ctx, p.pool, func(tx pgx.Tx) error {
		tag, err := tx.Exec(ctx,
			`DELETE FROM event_staff s USING events e
			 WHERE s.event_id = e.id AND e.id = $1 AND e.organizer_id = $2 AND s.user_id = $3`,
			eventID, organizerID, userID)
		if err != nil {
			return err
		}
		if tag.RowsAffected() == 0 {
			return ErrNotFound
		}
		return writeAudit(ctx, tx, auditEntry{eventID: eventID, actorID: actorID, action: "staff.removed",
			entityType: "user", entityID: userID, details: map[string]any{}})
	})
}
