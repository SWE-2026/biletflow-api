package store

import (
	"context"
	"errors"

	"github.com/jackc/pgx/v5"
)

var ErrInvalidAssignee = errors.New("assignee cannot handle this case")

type SupportCaseInput struct {
	Kind     string  `json:"kind"`
	Category string  `json:"category"`
	Subject  string  `json:"subject"`
	Message  string  `json:"message"`
	EventID  *string `json:"eventId"`
	OrderID  *string `json:"orderId"`
	TicketID *string `json:"ticketId"`
}

type caseContext struct {
	organizerID string
	eventID     *string
	orderID     *string
	ticketID    *string
}

func attendeeCaseContext(ctx context.Context, tx pgx.Tx, userID string, in SupportCaseInput) (caseContext, error) {
	cc := caseContext{eventID: in.EventID, orderID: in.OrderID, ticketID: in.TicketID}
	var err error
	switch {
	case in.TicketID != nil:
		err = tx.QueryRow(ctx,
			`SELECT o.event_id::text, o.id::text, e.organizer_id FROM tickets t
			 JOIN order_items oi ON oi.id = t.order_item_id JOIN orders o ON o.id = oi.order_id
			 JOIN events e ON e.id = o.event_id WHERE t.id = $1 AND t.owner_id = $2`,
			*in.TicketID, userID).Scan(&cc.eventID, &cc.orderID, &cc.organizerID)
	case in.OrderID != nil:
		err = tx.QueryRow(ctx,
			`SELECT o.event_id::text, e.organizer_id FROM orders o JOIN events e ON e.id = o.event_id
			 WHERE o.id = $1 AND o.buyer_id = $2`, *in.OrderID, userID).Scan(&cc.eventID, &cc.organizerID)
	default:
		err = tx.QueryRow(ctx,
			`SELECT organizer_id FROM events WHERE id = $1
			 AND status IN ('published','cancelled') AND visibility IN ('public','unlisted')`,
			*in.EventID).Scan(&cc.organizerID)
	}
	return cc, notFound(err)
}

func organizerCaseContext(ctx context.Context, tx pgx.Tx, v SupportViewer, in SupportCaseInput) (caseContext, error) {
	cc := caseContext{organizerID: v.OrganizerID, eventID: in.EventID}
	if v.OrganizerID == "" {
		return cc, ErrForbidden
	}
	if in.EventID == nil {
		return cc, nil
	}
	var found bool
	err := tx.QueryRow(ctx, `SELECT true FROM events WHERE id = $1 AND organizer_id = $2`,
		*in.EventID, v.OrganizerID).Scan(&found)
	return cc, notFound(err)
}

func (p *Postgres) CreateSupportCase(ctx context.Context, v SupportViewer, in SupportCaseInput) (SupportCase, error) {
	var c SupportCase
	err := pgx.BeginFunc(ctx, p.pool, func(tx pgx.Tx) error {
		resolve := attendeeCaseContext
		if in.Kind == "organizer" {
			resolve = func(ctx context.Context, tx pgx.Tx, _ string, in SupportCaseInput) (caseContext, error) {
				return organizerCaseContext(ctx, tx, v, in)
			}
		}
		cc, err := resolve(ctx, tx, v.UserID, in)
		if err != nil {
			return err
		}
		var id string
		if err := tx.QueryRow(ctx,
			`INSERT INTO support_cases (kind, requester_id, organizer_id, event_id, order_id, ticket_id,
			 category, subject, last_activity_by) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $2) RETURNING id`,
			in.Kind, v.UserID, cc.organizerID, cc.eventID, cc.orderID, cc.ticketID, in.Category, in.Subject).
			Scan(&id); err != nil {
			return err
		}
		if err := insertSupportMessage(ctx, tx, id, v.UserID, "message", in.Message); err != nil {
			return err
		}
		if err := markCaseRead(ctx, tx, id, v.UserID); err != nil {
			return err
		}
		c, err = supportCase(ctx, tx, v, id)
		return err
	})
	return c, err
}

func insertSupportMessage(ctx context.Context, tx pgx.Tx, caseID, authorID, kind, body string) error {
	_, err := tx.Exec(ctx,
		`INSERT INTO support_messages (case_id, author_id, kind, body) VALUES ($1, $2, $3, $4)`,
		caseID, authorID, kind, body)
	return err
}

type lockedCase struct {
	kind        string
	organizerID string
	status      string
	role        string
	eventID     *string
}

func lockCase(ctx context.Context, tx pgx.Tx, v SupportViewer, caseID string) (lockedCase, error) {
	var lc lockedCase
	err := tx.QueryRow(ctx,
		`SELECT c.kind, c.organizer_id::text, c.status,
		 CASE WHEN c.requester_id = $1 THEN 'requester' ELSE 'handler' END, c.event_id::text
		 FROM support_cases c WHERE c.id = $4 AND `+caseAccess+` FOR UPDATE`,
		append(v.args(), caseID)...).Scan(&lc.kind, &lc.organizerID, &lc.status, &lc.role, &lc.eventID)
	return lc, notFound(err)
}

func touchCase(ctx context.Context, tx pgx.Tx, caseID, actorID, status string) error {
	if _, err := tx.Exec(ctx,
		`UPDATE support_cases SET status = $3, last_activity_at = now(), last_activity_by = $2, updated_at = now()
		 WHERE id = $1`, caseID, actorID, status); err != nil {
		return err
	}
	return markCaseRead(ctx, tx, caseID, actorID)
}

func (p *Postgres) AddSupportMessage(ctx context.Context, v SupportViewer, caseID, body string) (SupportCase, error) {
	return p.updateCase(ctx, v, caseID, func(tx pgx.Tx, lc lockedCase) error {
		status := lc.status
		if lc.role == "requester" && (status == "waiting_for_customer" || status == "resolved") {
			status = "open"
		}
		if err := insertSupportMessage(ctx, tx, caseID, v.UserID, "message", body); err != nil {
			return err
		}
		return touchCase(ctx, tx, caseID, v.UserID, status)
	})
}

func (p *Postgres) SetSupportStatus(ctx context.Context, v SupportViewer, caseID, status string) (SupportCase, error) {
	return p.updateCase(ctx, v, caseID, func(tx pgx.Tx, lc lockedCase) error {
		if lc.role != "handler" {
			return ErrForbidden
		}
		if err := insertSupportMessage(ctx, tx, caseID, v.UserID, "status_change", status); err != nil {
			return err
		}
		if lc.eventID != nil {
			if err := writeAudit(ctx, tx, auditEntry{eventID: *lc.eventID, actorID: v.UserID, action: "support.status_changed",
				entityType: "support_case", entityID: caseID, details: map[string]any{"status": status}}); err != nil {
				return err
			}
		}
		return touchCase(ctx, tx, caseID, v.UserID, status)
	})
}

func (p *Postgres) AssignSupportCase(ctx context.Context, v SupportViewer, caseID string, assigneeID *string) (SupportCase, error) {
	return p.updateCase(ctx, v, caseID, func(tx pgx.Tx, lc lockedCase) error {
		if lc.role != "handler" {
			return ErrForbidden
		}
		var email string
		if assigneeID != nil {
			err := tx.QueryRow(ctx,
				`SELECT email FROM users WHERE id = $1
				 AND (role = 'admin' OR ($2 = 'attendee' AND organizer_id::text = $3))`,
				*assigneeID, lc.kind, lc.organizerID).Scan(&email)
			if errors.Is(err, pgx.ErrNoRows) {
				return ErrInvalidAssignee
			}
			if err != nil {
				return err
			}
		}
		if _, err := tx.Exec(ctx, `UPDATE support_cases SET assignee_id = $2 WHERE id = $1`, caseID, assigneeID); err != nil {
			return err
		}
		if lc.eventID != nil {
			if err := writeAudit(ctx, tx, auditEntry{eventID: *lc.eventID, actorID: v.UserID, action: "support.assigned",
				entityType: "support_case", entityID: caseID, details: map[string]any{"assignee": email}}); err != nil {
				return err
			}
		}
		if err := insertSupportMessage(ctx, tx, caseID, v.UserID, "assignment", email); err != nil {
			return err
		}
		return touchCase(ctx, tx, caseID, v.UserID, lc.status)
	})
}

func (p *Postgres) updateCase(ctx context.Context, v SupportViewer, caseID string, fn func(tx pgx.Tx, lc lockedCase) error) (SupportCase, error) {
	var c SupportCase
	err := pgx.BeginFunc(ctx, p.pool, func(tx pgx.Tx) error {
		lc, err := lockCase(ctx, tx, v, caseID)
		if err != nil {
			return err
		}
		if err := fn(tx, lc); err != nil {
			return err
		}
		c, err = supportCase(ctx, tx, v, caseID)
		return err
	})
	return c, err
}
