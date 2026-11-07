package store

import (
	"context"
	"errors"
	"time"

	"github.com/jackc/pgx/v5"
)

var ErrAlreadyDisputed = errors.New("order already has an open dispute")

type Dispute struct {
	ID             string     `json:"id"`
	OrderID        string     `json:"orderId"`
	EventID        string     `json:"eventId"`
	EventTitle     string     `json:"eventTitle"`
	BuyerEmail     string     `json:"buyerEmail"`
	Amount         int64      `json:"amount"`
	Reason         string     `json:"reason"`
	Details        string     `json:"details"`
	Status         string     `json:"status"`
	ResolutionNote string     `json:"resolutionNote"`
	Demo           bool       `json:"demo"`
	CreatedAt      time.Time  `json:"createdAt"`
	ResolvedAt     *time.Time `json:"resolvedAt"`
}

const disputeSelect = `SELECT d.id, o.id, e.id, e.title, u.email, o.total_amount, d.reason, d.details,
 d.status, d.resolution_note, d.created_at, d.resolved_at
 FROM disputes d JOIN orders o ON o.id = d.order_id JOIN events e ON e.id = o.event_id
 JOIN users u ON u.id = o.buyer_id`

func scanDispute(row pgx.Row) (Dispute, error) {
	d := Dispute{Demo: true}
	err := row.Scan(&d.ID, &d.OrderID, &d.EventID, &d.EventTitle, &d.BuyerEmail, &d.Amount, &d.Reason,
		&d.Details, &d.Status, &d.ResolutionNote, &d.CreatedAt, &d.ResolvedAt)
	return d, notFound(err)
}

func collectDisputes(ctx context.Context, q querier, sql string, args ...any) ([]Dispute, error) {
	rows, err := q.Query(ctx, sql, args...)
	if err != nil {
		return nil, err
	}
	return pgx.CollectRows(rows, func(row pgx.CollectableRow) (Dispute, error) { return scanDispute(row) })
}

func (p *Postgres) OpenDispute(ctx context.Context, userID, orderID, reason, details string) (Dispute, error) {
	var d Dispute
	err := pgx.BeginFunc(ctx, p.pool, func(tx pgx.Tx) error {
		var status, eventID string
		var total int64
		if err := tx.QueryRow(ctx,
			`SELECT status, total_amount, event_id FROM orders WHERE id = $1 AND buyer_id = $2 FOR UPDATE`,
			orderID, userID).Scan(&status, &total, &eventID); err != nil {
			return notFound(err)
		}
		if status != "paid" {
			return ErrOrderState
		}
		if total == 0 {
			return ErrFreeOrder
		}
		var id string
		err := tx.QueryRow(ctx,
			`INSERT INTO disputes (order_id, opened_by, reason, details) VALUES ($1, $2, $3, $4) RETURNING id`,
			orderID, userID, reason, details).Scan(&id)
		if IsUniqueViolation(err) {
			return ErrAlreadyDisputed
		}
		if err != nil {
			return err
		}
		if err := writeAudit(ctx, tx, auditEntry{eventID: eventID, actorID: userID, action: "dispute.opened",
			entityType: "dispute", entityID: id, details: map[string]any{"orderId": orderID, "reason": reason}}); err != nil {
			return err
		}
		d, err = scanDispute(tx.QueryRow(ctx, disputeSelect+` WHERE d.id = $1`, id))
		return err
	})
	return d, err
}

func (p *Postgres) UserDisputes(ctx context.Context, userID string) ([]Dispute, error) {
	return collectDisputes(ctx, p.pool, disputeSelect+` WHERE o.buyer_id = $1 ORDER BY d.created_at DESC, d.id`, userID)
}

func (p *Postgres) EventDisputes(ctx context.Context, organizerID, eventID string) ([]Dispute, error) {
	if _, err := p.EventByOwner(ctx, organizerID, eventID); err != nil {
		return nil, err
	}
	return collectDisputes(ctx, p.pool, disputeSelect+` WHERE o.event_id = $1 ORDER BY d.created_at DESC, d.id`, eventID)
}

func (p *Postgres) AdminDisputes(ctx context.Context, status string) ([]Dispute, error) {
	return collectDisputes(ctx, p.pool, disputeSelect+` WHERE $1 = '' OR d.status = $1
		 ORDER BY d.created_at DESC, d.id LIMIT $2`, status, AdminListLimit)
}

func (p *Postgres) ResolveDispute(ctx context.Context, actorID, disputeID string, refund bool, note string) (Dispute, error) {
	var d Dispute
	err := pgx.BeginFunc(ctx, p.pool, func(tx pgx.Tx) error {
		var orderID, eventID, orderStatus string
		var total int64
		err := tx.QueryRow(ctx,
			`SELECT o.id, o.event_id, o.status, o.total_amount FROM disputes d JOIN orders o ON o.id = d.order_id
			 WHERE d.id = $1 AND d.status = 'open' FOR UPDATE OF d, o`, disputeID).Scan(&orderID, &eventID, &orderStatus, &total)
		if errors.Is(err, pgx.ErrNoRows) {
			if _, lookupErr := scanDispute(tx.QueryRow(ctx, disputeSelect+` WHERE d.id = $1`, disputeID)); lookupErr != nil {
				return lookupErr
			}
			return ErrStateConflict
		}
		if err != nil {
			return err
		}
		status := "rejected"
		if refund {
			status = "refunded"
			if orderStatus == "paid" {
				if err := refundPaidOrder(ctx, tx, actorID, eventID, orderID, "Dispute: "+note, total); err != nil {
					return err
				}
			} else if orderStatus != "refunded" {
				return ErrOrderState
			}
		}
		if _, err := tx.Exec(ctx,
			`UPDATE disputes SET status = $2, resolution_note = $3, resolved_at = now(), resolved_by = $4 WHERE id = $1`,
			disputeID, status, note, actorID); err != nil {
			return err
		}
		if err := writeAudit(ctx, tx, auditEntry{eventID: eventID, actorID: actorID, action: "dispute." + status,
			entityType: "dispute", entityID: disputeID, details: map[string]any{"orderId": orderID}}); err != nil {
			return err
		}
		d, err = scanDispute(tx.QueryRow(ctx, disputeSelect+` WHERE d.id = $1`, disputeID))
		return err
	})
	return d, err
}
