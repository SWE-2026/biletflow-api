package store

import (
	"context"
	"errors"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
)

var (
	ErrForbidden    = errors.New("role does not allow this action")
	ErrNotCheckedIn = errors.New("ticket is not checked in")
)

const maxSearchResults = 50

type StaffTicket struct {
	ID             string     `json:"id"`
	Status         string     `json:"status"`
	TicketTypeName string     `json:"ticketTypeName"`
	AttendeeName   string     `json:"attendeeName"`
	AttendeeEmail  string     `json:"attendeeEmail"`
	OrderID        string     `json:"orderId"`
	CheckedInAt    *time.Time `json:"checkedInAt"`
}

type CheckInResult struct {
	Result string       `json:"result"`
	Ticket *StaffTicket `json:"ticket"`
}

type CheckInSummary struct {
	Registered int `json:"registered"`
	CheckedIn  int `json:"checkedIn"`
	Remaining  int `json:"remaining"`
}

const staffTicketSelect = `SELECT t.id, t.status, tt.name,
 COALESCE(NULLIF(o.attendee_name, ''), NULLIF(u.display_name, ''), u.email),
 COALESCE(NULLIF(o.attendee_email, ''), u.email), o.id, c.checked_in_at
 FROM tickets t
 JOIN order_items oi ON oi.id = t.order_item_id
 JOIN orders o ON o.id = oi.order_id
 JOIN ticket_types tt ON tt.id = oi.ticket_type_id
 JOIN users u ON u.id = t.owner_id
 LEFT JOIN check_ins c ON c.ticket_id = t.id AND c.reversed_at IS NULL
 WHERE o.event_id = $1`

const (
	byToken = ` AND t.qr_token = $2`
	byID    = ` AND t.id = $2::uuid`
)

func scanStaffTicket(row pgx.Row) (StaffTicket, error) {
	var t StaffTicket
	err := row.Scan(&t.ID, &t.Status, &t.TicketTypeName, &t.AttendeeName, &t.AttendeeEmail, &t.OrderID, &t.CheckedInAt)
	return t, err
}

func staffRole(ctx context.Context, q rowQuerier, userID, eventID string) (role, status string, err error) {
	err = q.QueryRow(ctx,
		`SELECT CASE WHEN e.organizer_id = u.organizer_id THEN 'owner' ELSE s.role END, e.status
		 FROM events e JOIN users u ON u.id = $2
		 LEFT JOIN event_staff s ON s.event_id = e.id AND s.user_id = u.id
		 WHERE e.id = $1 AND (e.organizer_id = u.organizer_id OR s.user_id IS NOT NULL)`,
		eventID, userID).Scan(&role, &status)
	return role, status, notFound(err)
}

func requireCheckInAccess(ctx context.Context, tx pgx.Tx, userID, eventID string) (string, error) {
	role, status, err := staffRole(ctx, tx, userID, eventID)
	if err == nil && status != "published" {
		err = ErrInvalidEventState
	}
	return role, err
}

func (p *Postgres) ListStaffEvents(ctx context.Context, userID string) ([]Event, error) {
	rows, err := p.pool.Query(ctx,
		`SELECT `+eventColumns+` FROM events e
		 WHERE e.status = 'published' AND (
		 e.organizer_id = (SELECT organizer_id FROM users WHERE id = $1)
		 OR EXISTS (SELECT 1 FROM event_staff s WHERE s.event_id = e.id AND s.user_id = $1))
		 ORDER BY e.starts_at, e.id`, userID)
	if err != nil {
		return nil, err
	}
	return pgx.CollectRows(rows, func(row pgx.CollectableRow) (Event, error) { return scanEvent(row) })
}

func (p *Postgres) StaffEvent(ctx context.Context, userID, eventID string) (Event, string, CheckInSummary, error) {
	var sum CheckInSummary
	role, _, err := staffRole(ctx, p.pool, userID, eventID)
	if err != nil {
		return Event{}, "", sum, err
	}
	e, err := scanEvent(p.pool.QueryRow(ctx, `SELECT `+eventColumns+` FROM events WHERE id = $1`, eventID))
	if err != nil {
		return Event{}, "", sum, err
	}
	err = p.pool.QueryRow(ctx,
		`SELECT COUNT(*) FILTER (WHERE t.status IN ('valid','used')), COUNT(*) FILTER (WHERE t.status = 'used')
		 FROM tickets t JOIN order_items oi ON oi.id = t.order_item_id JOIN orders o ON o.id = oi.order_id
		 WHERE o.event_id = $1`, eventID).Scan(&sum.Registered, &sum.CheckedIn)
	sum.Remaining = sum.Registered - sum.CheckedIn
	return e, role, sum, err
}

func likePattern(q string) string {
	return "%" + strings.NewReplacer(`\`, `\\`, `%`, `\%`, `_`, `\_`).Replace(q) + "%"
}

func (p *Postgres) SearchAttendees(ctx context.Context, userID, eventID, query string) ([]StaffTicket, error) {
	if _, _, err := staffRole(ctx, p.pool, userID, eventID); err != nil {
		return nil, err
	}
	rows, err := p.pool.Query(ctx, staffTicketSelect+` AND ($2 = ''
		 OR o.attendee_name ILIKE $3 OR o.attendee_email ILIKE $3
		 OR u.display_name ILIKE $3 OR u.email ILIKE $3 OR t.id::text = $2)
		 ORDER BY 4, t.id LIMIT $4`, eventID, query, likePattern(query), maxSearchResults)
	if err != nil {
		return nil, err
	}
	return pgx.CollectRows(rows, func(row pgx.CollectableRow) (StaffTicket, error) { return scanStaffTicket(row) })
}

func (p *Postgres) CheckInByToken(ctx context.Context, userID, eventID, token string) (CheckInResult, error) {
	return p.checkIn(ctx, userID, eventID, byToken, token)
}

func (p *Postgres) CheckInByID(ctx context.Context, userID, eventID, ticketID string) (CheckInResult, error) {
	return p.checkIn(ctx, userID, eventID, byID, ticketID)
}

func (p *Postgres) checkIn(ctx context.Context, userID, eventID, match, value string) (CheckInResult, error) {
	var res CheckInResult
	err := pgx.BeginFunc(ctx, p.pool, func(tx pgx.Tx) error {
		if _, err := requireCheckInAccess(ctx, tx, userID, eventID); err != nil {
			return err
		}
		var ticketID string
		err := tx.QueryRow(ctx,
			`UPDATE tickets t SET status = 'used'
			 FROM order_items oi JOIN orders o ON o.id = oi.order_id
			 WHERE oi.id = t.order_item_id AND o.event_id = $1`+match+` AND t.status = 'valid'
			 RETURNING t.id`, eventID, value).Scan(&ticketID)
		switch {
		case err == nil:
			if _, err := tx.Exec(ctx,
				`INSERT INTO check_ins (ticket_id, checked_in_by, checked_in_at) VALUES ($1, $2, now())`,
				ticketID, userID); err != nil {
				return err
			}
		case !errors.Is(err, pgx.ErrNoRows):
			return err
		}
		ticket, err := scanStaffTicket(tx.QueryRow(ctx, staffTicketSelect+match, eventID, value))
		if errors.Is(err, pgx.ErrNoRows) {
			res.Result = "invalid"
			return nil
		}
		res.Result, res.Ticket = checkInResult(ticketID != "", ticket.Status), &ticket
		return err
	})
	return res, err
}

func checkInResult(admitted bool, status string) string {
	switch {
	case admitted:
		return "valid"
	case status == "used":
		return "already_used"
	default:
		return status
	}
}

func (p *Postgres) UndoCheckIn(ctx context.Context, userID, eventID, ticketID string) (StaffTicket, error) {
	var ticket StaffTicket
	err := pgx.BeginFunc(ctx, p.pool, func(tx pgx.Tx) error {
		role, err := requireCheckInAccess(ctx, tx, userID, eventID)
		if err != nil {
			return err
		}
		if role != "owner" && role != "supervisor" {
			return ErrForbidden
		}
		tag, err := tx.Exec(ctx,
			`UPDATE tickets t SET status = 'valid'
			 FROM order_items oi JOIN orders o ON o.id = oi.order_id
			 WHERE oi.id = t.order_item_id AND o.event_id = $1 AND t.id = $2::uuid AND t.status = 'used'`,
			eventID, ticketID)
		if err != nil {
			return err
		}
		if tag.RowsAffected() == 0 {
			_, err := scanStaffTicket(tx.QueryRow(ctx, staffTicketSelect+byID, eventID, ticketID))
			if err == nil {
				return ErrNotCheckedIn
			}
			return notFound(err)
		}
		if _, err := tx.Exec(ctx,
			`UPDATE check_ins SET reversed_at = now(), reversed_by = $2 WHERE ticket_id = $1 AND reversed_at IS NULL`,
			ticketID, userID); err != nil {
			return err
		}
		if err := writeAudit(ctx, tx, auditEntry{eventID: eventID, actorID: userID, action: "check_in.reversed",
			entityType: "ticket", entityID: ticketID, details: map[string]any{}}); err != nil {
			return err
		}
		ticket, err = scanStaffTicket(tx.QueryRow(ctx, staffTicketSelect+byID, eventID, ticketID))
		return err
	})
	return ticket, err
}
