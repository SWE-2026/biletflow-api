package store

import (
	"context"
	"errors"
	"time"

	"github.com/jackc/pgx/v5"
)

var (
	ErrStateConflict   = errors.New("state does not allow this action")
	ErrAlreadyReported = errors.New("event already reported by this user")
)

type EventReport struct {
	ID            string     `json:"id"`
	EventID       string     `json:"eventId"`
	EventTitle    string     `json:"eventTitle"`
	EventStatus   string     `json:"eventStatus"`
	ReporterEmail string     `json:"reporterEmail"`
	Reason        string     `json:"reason"`
	Details       string     `json:"details"`
	Status        string     `json:"status"`
	CreatedAt     time.Time  `json:"createdAt"`
	ReviewedAt    *time.Time `json:"reviewedAt"`
}

func (p *Postgres) PromoteAdmins(ctx context.Context, emails []string) (int64, error) {
	tag, err := p.pool.Exec(ctx,
		`UPDATE users SET role = 'admin' WHERE email = ANY($1::text[]) AND role = 'customer'`, emails)
	return tag.RowsAffected(), err
}

func (p *Postgres) SetUserSuspended(ctx context.Context, actorID, userID string, suspend bool) (AdminUser, error) {
	err := pgx.BeginFunc(ctx, p.pool, func(tx pgx.Tx) error {
		var role string
		if err := tx.QueryRow(ctx, `SELECT role FROM users WHERE id = $1 FOR UPDATE`, userID).Scan(&role); err != nil {
			return notFound(err)
		}
		if role == "admin" {
			return ErrForbidden
		}
		tag, err := tx.Exec(ctx,
			`UPDATE users SET suspended_at = CASE WHEN $2 THEN now() END
			 WHERE id = $1 AND (suspended_at IS NULL) = $2`, userID, suspend)
		if err != nil {
			return err
		}
		if tag.RowsAffected() == 0 {
			return ErrStateConflict
		}
		if suspend {
			if _, err := tx.Exec(ctx, `UPDATE sessions SET revoked_at = now() WHERE user_id = $1 AND revoked_at IS NULL`, userID); err != nil {
				return err
			}
		}
		action := "user.restored"
		if suspend {
			action = "user.suspended"
		}
		return writeAudit(ctx, tx, auditEntry{actorID: actorID, action: action, entityType: "user",
			entityID: userID, details: map[string]any{}})
	})
	if err != nil {
		return AdminUser{}, err
	}
	users, err := p.AdminUsers(ctx, userID)
	if err == nil && len(users) == 0 {
		err = ErrNotFound
	}
	if err != nil {
		return AdminUser{}, err
	}
	return users[0], nil
}

func (p *Postgres) SetEventSuspended(ctx context.Context, actorID, eventID string, suspend bool) (Event, error) {
	from, to, action := "suspended", "published", "event.restored"
	if suspend {
		from, to, action = "published", "suspended", "event.suspended"
	}
	var e Event
	err := pgx.BeginFunc(ctx, p.pool, func(tx pgx.Tx) error {
		var err error
		e, err = scanEvent(tx.QueryRow(ctx,
			`UPDATE events SET status = $3, updated_at = now() WHERE id = $1 AND status = $2 RETURNING `+eventColumns,
			eventID, from, to))
		if err != nil {
			return err
		}
		return writeAudit(ctx, tx, auditEntry{eventID: eventID, actorID: actorID, action: action,
			entityType: "event", entityID: eventID, details: map[string]any{"status": to}})
	})
	if errors.Is(err, ErrNotFound) {
		if _, lookupErr := p.AdminEvent(ctx, eventID); lookupErr == nil {
			err = ErrInvalidEventState
		}
	}
	return e, err
}

func (p *Postgres) SetPaidSalesSuspended(ctx context.Context, actorID, eventID string, suspend bool) error {
	from, to, action := "suspended", "active", "paid_sales.restored"
	if suspend {
		from, to, action = "active", "suspended", "paid_sales.suspended"
	}
	return pgx.BeginFunc(ctx, p.pool, func(tx pgx.Tx) error {
		var id string
		err := tx.QueryRow(ctx,
			`UPDATE paid_sales_activations SET status = $3, reviewed_by = $4, reviewed_at = now(),
			 suspended_at = CASE WHEN $3 = 'suspended' THEN now() ELSE suspended_at END
			 WHERE event_id = $1 AND status = $2 RETURNING id`, eventID, from, to, actorID).Scan(&id)
		if errors.Is(err, pgx.ErrNoRows) {
			return ErrStateConflict
		}
		if err != nil {
			return err
		}
		return writeAudit(ctx, tx, auditEntry{eventID: eventID, actorID: actorID, action: action,
			entityType: "paid_sales_activation", entityID: id, details: map[string]any{}})
	})
}

func (p *Postgres) ReportEvent(ctx context.Context, userID, eventID, reason, details string) error {
	if _, err := p.PublicEvent(ctx, eventID); err != nil {
		return err
	}
	_, err := p.pool.Exec(ctx,
		`INSERT INTO event_reports (event_id, reporter_id, reason, details) VALUES ($1, $2, $3, $4)`,
		eventID, userID, reason, details)
	if IsUniqueViolation(err) {
		return ErrAlreadyReported
	}
	return err
}

const reportSelect = `SELECT r.id, e.id, e.title, e.status, u.email, r.reason, r.details, r.status,
 r.created_at, r.reviewed_at
 FROM event_reports r JOIN events e ON e.id = r.event_id JOIN users u ON u.id = r.reporter_id`

func scanReport(row pgx.Row) (EventReport, error) {
	var r EventReport
	err := row.Scan(&r.ID, &r.EventID, &r.EventTitle, &r.EventStatus, &r.ReporterEmail, &r.Reason,
		&r.Details, &r.Status, &r.CreatedAt, &r.ReviewedAt)
	return r, notFound(err)
}

func (p *Postgres) EventReports(ctx context.Context, status string) ([]EventReport, error) {
	rows, err := p.pool.Query(ctx, reportSelect+` WHERE $1 = '' OR r.status = $1
		 ORDER BY r.created_at DESC, r.id LIMIT $2`, status, AdminListLimit)
	if err != nil {
		return nil, err
	}
	return pgx.CollectRows(rows, func(row pgx.CollectableRow) (EventReport, error) { return scanReport(row) })
}

func (p *Postgres) ResolveReport(ctx context.Context, actorID, reportID, status string) (EventReport, error) {
	var r EventReport
	err := pgx.BeginFunc(ctx, p.pool, func(tx pgx.Tx) error {
		var eventID string
		err := tx.QueryRow(ctx,
			`UPDATE event_reports SET status = $2, reviewed_at = now(), reviewed_by = $3
			 WHERE id = $1 AND status = 'open' RETURNING event_id`, reportID, status, actorID).Scan(&eventID)
		if errors.Is(err, pgx.ErrNoRows) {
			var exists bool
			if err := tx.QueryRow(ctx, `SELECT true FROM event_reports WHERE id = $1`, reportID).Scan(&exists); err != nil {
				return notFound(err)
			}
			return ErrStateConflict
		}
		if err != nil {
			return err
		}
		if err := writeAudit(ctx, tx, auditEntry{eventID: eventID, actorID: actorID, action: "report." + status,
			entityType: "event_report", entityID: reportID, details: map[string]any{}}); err != nil {
			return err
		}
		r, err = scanReport(tx.QueryRow(ctx, reportSelect+` WHERE r.id = $1`, reportID))
		return err
	})
	return r, err
}
