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

// scanMeta describes the scan behind a check-in. The zero value is a live scan without an ID,
// recorded at the server's current time.
type scanMeta struct {
	// id is the scanner's own ID for the scan; a repeat of it returns the first outcome.
	id string
	// at is when an offline scanner admitted the ticket; nil means now.
	at *time.Time
}

// CheckInByToken checks in the ticket with this QR token. scanID is optional (empty for none).
func (p *Postgres) CheckInByToken(ctx context.Context, userID, eventID, token, scanID string) (CheckInResult, error) {
	return p.checkIn(ctx, userID, eventID, byToken, token, scanMeta{id: scanID})
}

func (p *Postgres) CheckInByID(ctx context.Context, userID, eventID, ticketID string) (CheckInResult, error) {
	return p.checkIn(ctx, userID, eventID, byID, ticketID, scanMeta{})
}

func (p *Postgres) checkIn(ctx context.Context, userID, eventID, match, value string, scan scanMeta) (CheckInResult, error) {
	var res CheckInResult
	err := pgx.BeginFunc(ctx, p.pool, func(tx pgx.Tx) error {
		if _, err := requireCheckInAccess(ctx, tx, userID, eventID); err != nil {
			return err
		}
		replayed, found, err := replayedScan(ctx, tx, userID, eventID, scan.id)
		if err != nil || found {
			res = replayed
			return err
		}
		admitted, err := admitTicket(ctx, tx, userID, eventID, match, value, scan)
		if err != nil {
			return err
		}
		res, err = describeCheckIn(ctx, tx, eventID, match, value, admitted)
		return err
	})
	return res, err
}

// replayedScan answers a scan ID this user already sent: the check-in it created counts as
// admitted, so a scanner that lost the first response does not report a false duplicate.
func replayedScan(ctx context.Context, tx pgx.Tx, userID, eventID, scanID string) (CheckInResult, bool, error) {
	if scanID == "" {
		return CheckInResult{}, false, nil
	}
	var ticketID string
	err := tx.QueryRow(ctx, `SELECT ticket_id FROM check_ins WHERE scan_id = $1 AND checked_in_by = $2`,
		scanID, userID).Scan(&ticketID)
	if errors.Is(err, pgx.ErrNoRows) {
		return CheckInResult{}, false, nil
	}
	if err != nil {
		return CheckInResult{}, false, err
	}
	ticket, err := scanStaffTicket(tx.QueryRow(ctx, staffTicketSelect+byID, eventID, ticketID))
	if errors.Is(err, pgx.ErrNoRows) {
		return CheckInResult{Result: "invalid"}, true, nil
	}
	return CheckInResult{Result: "valid", Ticket: &ticket}, true, err
}

// admitTicket marks a valid ticket used and records the check-in; false when the ticket is not
// valid (or not found). The guarded UPDATE admits a ticket at most once under concurrent scans.
func admitTicket(ctx context.Context, tx pgx.Tx, userID, eventID, match, value string, scan scanMeta) (bool, error) {
	var ticketID string
	err := tx.QueryRow(ctx,
		`UPDATE tickets t SET status = 'used'
		 FROM order_items oi JOIN orders o ON o.id = oi.order_id
		 WHERE oi.id = t.order_item_id AND o.event_id = $1`+match+` AND t.status = 'valid'
		 RETURNING t.id`, eventID, value).Scan(&ticketID)
	if errors.Is(err, pgx.ErrNoRows) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	if _, err := tx.Exec(ctx,
		`INSERT INTO check_ins (ticket_id, checked_in_by, checked_in_at, scan_id)
		 VALUES ($1, $2, COALESCE($3::timestamptz, now()), NULLIF($4, '')::uuid)`,
		ticketID, userID, scan.at, scan.id); err != nil {
		return false, err
	}
	details := map[string]any{}
	if scan.at != nil {
		details = map[string]any{"offline": true, "scannedAt": scan.at.UTC().Format(time.RFC3339)}
	}
	return true, writeAudit(ctx, tx, auditEntry{eventID: eventID, actorID: userID, action: "check_in.recorded",
		entityType: "ticket", entityID: ticketID, details: details})
}

func describeCheckIn(ctx context.Context, tx pgx.Tx, eventID, match, value string, admitted bool) (CheckInResult, error) {
	ticket, err := scanStaffTicket(tx.QueryRow(ctx, staffTicketSelect+match, eventID, value))
	if errors.Is(err, pgx.ErrNoRows) {
		res := CheckInResult{Result: "invalid"}
		if match == byToken && isCampaignToken(ctx, tx, value) {
			res.Result = "campaign_qr"
		}
		return res, nil
	}
	if err != nil {
		return CheckInResult{}, err
	}
	return CheckInResult{Result: checkInResult(admitted, ticket.Status), Ticket: &ticket}, nil
}

func isCampaignToken(ctx context.Context, tx pgx.Tx, value string) bool {
	if strings.Contains(value, "campaign=") {
		return true
	}
	var found bool
	err := tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM campaigns WHERE token = $1)`, value).Scan(&found)
	return err == nil && found
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

const maxAttendeeList = 5000

type AttendeeFilter struct {
	TicketTypeID string
	Status       string
	Query        string
}

func (p *Postgres) ListEventAttendees(ctx context.Context, organizerID, eventID string, f AttendeeFilter) ([]StaffTicket, error) {
	if _, err := p.EventByOwner(ctx, organizerID, eventID); err != nil {
		return nil, err
	}
	rows, err := p.pool.Query(ctx, staffTicketSelect+`
		 AND ($2 = '' OR tt.id::text = $2) AND ($3 = '' OR t.status = $3)
		 AND ($4 = '' OR o.attendee_name ILIKE $5 OR o.attendee_email ILIKE $5
		 OR u.display_name ILIKE $5 OR u.email ILIKE $5)
		 ORDER BY 4, t.id LIMIT $6`,
		eventID, f.TicketTypeID, f.Status, f.Query, likePattern(f.Query), maxAttendeeList)
	if err != nil {
		return nil, err
	}
	return pgx.CollectRows(rows, func(row pgx.CollectableRow) (StaffTicket, error) { return scanStaffTicket(row) })
}
