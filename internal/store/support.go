package store

import (
	"context"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
)

const maxSupportCases = 100

type SupportViewer struct {
	UserID      string
	OrganizerID string
	Admin       bool
}

func (v SupportViewer) args() []any { return []any{v.UserID, v.OrganizerID, v.Admin} }

type SupportCase struct {
	ID             string    `json:"id"`
	Kind           string    `json:"kind"`
	Category       string    `json:"category"`
	Subject        string    `json:"subject"`
	Status         string    `json:"status"`
	RequesterID    string    `json:"requesterId"`
	RequesterEmail string    `json:"requesterEmail"`
	RequesterName  string    `json:"requesterName"`
	EventID        *string   `json:"eventId"`
	EventTitle     *string   `json:"eventTitle"`
	OrderID        *string   `json:"orderId"`
	TicketID       *string   `json:"ticketId"`
	AssigneeID     *string   `json:"assigneeId"`
	AssigneeEmail  *string   `json:"assigneeEmail"`
	ViewerRole     string    `json:"viewerRole"`
	Unread         bool      `json:"unread"`
	CreatedAt      time.Time `json:"createdAt"`
	UpdatedAt      time.Time `json:"updatedAt"`
	LastActivityAt time.Time `json:"lastActivityAt"`
}

type SupportMessage struct {
	ID         string    `json:"id"`
	Kind       string    `json:"kind"`
	AuthorID   string    `json:"authorId"`
	AuthorName string    `json:"authorName"`
	AuthorRole string    `json:"authorRole"`
	Body       string    `json:"body"`
	CreatedAt  time.Time `json:"createdAt"`
}

const caseAccess = `(c.requester_id = $1 OR (c.kind = 'attendee' AND c.organizer_id::text = $2) OR $3::bool)`

const caseSelect = `SELECT c.id, c.kind, c.category, c.subject, c.status, c.requester_id, ru.email,
 COALESCE(NULLIF(ru.display_name, ''), ru.email), c.event_id, e.title, c.order_id, c.ticket_id,
 c.assignee_id, au.email,
 CASE WHEN c.requester_id = $1 THEN 'requester' ELSE 'handler' END,
 c.last_activity_by <> $1 AND (r.last_read_at IS NULL OR r.last_read_at < c.last_activity_at),
 c.created_at, c.updated_at, c.last_activity_at
 FROM support_cases c
 JOIN users ru ON ru.id = c.requester_id
 LEFT JOIN events e ON e.id = c.event_id
 LEFT JOIN users au ON au.id = c.assignee_id
 LEFT JOIN support_case_reads r ON r.case_id = c.id AND r.user_id = $1
 WHERE ` + caseAccess

func scanCase(row pgx.Row) (SupportCase, error) {
	var c SupportCase
	err := row.Scan(&c.ID, &c.Kind, &c.Category, &c.Subject, &c.Status, &c.RequesterID, &c.RequesterEmail,
		&c.RequesterName, &c.EventID, &c.EventTitle, &c.OrderID, &c.TicketID, &c.AssigneeID, &c.AssigneeEmail,
		&c.ViewerRole, &c.Unread, &c.CreatedAt, &c.UpdatedAt, &c.LastActivityAt)
	return c, notFound(err)
}

func (p *Postgres) ListSupportCases(ctx context.Context, v SupportViewer, status, eventID string) ([]SupportCase, error) {
	rows, err := p.pool.Query(ctx, caseSelect+` AND ($4 = '' OR c.status = $4) AND ($6 = '' OR c.event_id::text = $6)
		 ORDER BY c.last_activity_at DESC, c.id LIMIT $5`, append(v.args(), status, maxSupportCases, eventID)...)
	if err != nil {
		return nil, err
	}
	return pgx.CollectRows(rows, func(row pgx.CollectableRow) (SupportCase, error) { return scanCase(row) })
}

func (p *Postgres) UnreadSupportCases(ctx context.Context, v SupportViewer) (int, error) {
	var n int
	err := p.pool.QueryRow(ctx,
		`SELECT COUNT(*) FROM support_cases c
		 LEFT JOIN support_case_reads r ON r.case_id = c.id AND r.user_id = $1
		 WHERE `+caseAccess+` AND c.last_activity_by <> $1
		 AND (r.last_read_at IS NULL OR r.last_read_at < c.last_activity_at)`, v.args()...).Scan(&n)
	return n, err
}

func supportCase(ctx context.Context, q rowQuerier, v SupportViewer, caseID string) (SupportCase, error) {
	return scanCase(q.QueryRow(ctx, caseSelect+` AND c.id = $4`, append(v.args(), caseID)...))
}

func (p *Postgres) SupportCase(ctx context.Context, v SupportViewer, caseID string) (SupportCase, []SupportMessage, error) {
	c, err := supportCase(ctx, p.pool, v, caseID)
	if err != nil {
		return c, nil, err
	}
	rows, err := p.pool.Query(ctx,
		`SELECT m.id, m.kind, m.author_id, COALESCE(NULLIF(u.display_name, ''), u.email),
		 CASE WHEN m.author_id = c.requester_id THEN 'requester' ELSE 'handler' END, m.body, m.created_at
		 FROM support_messages m JOIN support_cases c ON c.id = m.case_id JOIN users u ON u.id = m.author_id
		 WHERE m.case_id = $1 ORDER BY m.created_at, m.id`, caseID)
	if err != nil {
		return c, nil, err
	}
	messages, err := pgx.CollectRows(rows, func(row pgx.CollectableRow) (SupportMessage, error) {
		var m SupportMessage
		err := row.Scan(&m.ID, &m.Kind, &m.AuthorID, &m.AuthorName, &m.AuthorRole, &m.Body, &m.CreatedAt)
		return m, err
	})
	if err != nil {
		return c, nil, err
	}
	if err := markCaseRead(ctx, p.pool, caseID, v.UserID); err != nil {
		return c, nil, err
	}
	c.Unread = false
	return c, messages, nil
}

type execer interface {
	Exec(ctx context.Context, sql string, args ...any) (pgconn.CommandTag, error)
}

func markCaseRead(ctx context.Context, q execer, caseID, userID string) error {
	_, err := q.Exec(ctx,
		`INSERT INTO support_case_reads (case_id, user_id, last_read_at) VALUES ($1, $2, now())
		 ON CONFLICT (case_id, user_id) DO UPDATE SET last_read_at = now()`, caseID, userID)
	return err
}
