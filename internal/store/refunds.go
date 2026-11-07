package store

import (
	"context"
	"crypto/rand"
	"fmt"

	"github.com/jackc/pgx/v5"
)

type SalesSummary struct {
	GrossAmount           int64 `json:"grossAmount"`
	ProcessingFeeAmount   int64 `json:"processingFeeAmount"`
	RefundedAmount        int64 `json:"refundedAmount"`
	EstimatedPayoutAmount int64 `json:"estimatedPayoutAmount"`
	PaidOrders            int   `json:"paidOrders"`
	TicketsSold           int   `json:"ticketsSold"`
	Demo                  bool  `json:"demo"`
}

func (p *Postgres) EventSales(ctx context.Context, organizerID, eventID string) (SalesSummary, error) {
	s := SalesSummary{Demo: true}
	err := p.pool.QueryRow(ctx,
		`SELECT
		 (SELECT COALESCE(SUM(total_amount), 0)::bigint FROM orders WHERE event_id = e.id AND status IN ('paid','refunded')),
		 (SELECT COALESCE(SUM(processing_fee_amount), 0)::bigint FROM orders WHERE event_id = e.id AND status = 'paid'),
		 (SELECT COALESCE(SUM(total_amount), 0)::bigint FROM orders WHERE event_id = e.id AND status = 'refunded'),
		 (SELECT COUNT(*) FROM orders WHERE event_id = e.id AND status = 'paid' AND total_amount > 0),
		 (SELECT COUNT(*) FROM tickets t JOIN order_items oi ON oi.id = t.order_item_id
		  JOIN orders o ON o.id = oi.order_id WHERE o.event_id = e.id AND t.status IN ('valid','used'))
		 FROM events e WHERE e.id = $1 AND e.organizer_id = $2`, eventID, organizerID).
		Scan(&s.GrossAmount, &s.ProcessingFeeAmount, &s.RefundedAmount, &s.PaidOrders, &s.TicketsSold)
	s.EstimatedPayoutAmount = s.GrossAmount - s.RefundedAmount - s.ProcessingFeeAmount
	return s, notFound(err)
}

func lockEventOrder(ctx context.Context, tx pgx.Tx, organizerID, eventID, orderID string) (string, int64, error) {
	var status string
	var total int64
	err := tx.QueryRow(ctx,
		`SELECT o.status, o.total_amount FROM orders o JOIN events e ON e.id = o.event_id
		 WHERE o.id = $1 AND o.event_id = $2 AND e.organizer_id = $3 FOR UPDATE OF o`,
		orderID, eventID, organizerID).Scan(&status, &total)
	return status, total, notFound(err)
}

func invalidateTickets(ctx context.Context, tx pgx.Tx, orderID, status string) error {
	_, err := tx.Exec(ctx,
		`UPDATE tickets SET status = $2
		 WHERE order_item_id IN (SELECT id FROM order_items WHERE order_id = $1) AND status IN ('valid','used')`,
		orderID, status)
	return err
}

func (p *Postgres) RefundOrder(ctx context.Context, organizerID, actorID, eventID, orderID, reason string) (OrganizerOrder, error) {
	err := pgx.BeginFunc(ctx, p.pool, func(tx pgx.Tx) error {
		status, total, err := lockEventOrder(ctx, tx, organizerID, eventID, orderID)
		switch {
		case err != nil:
			return err
		case status != "paid":
			return ErrOrderState
		case total == 0:
			return ErrFreeOrder
		}
		return refundPaidOrder(ctx, tx, actorID, eventID, orderID, reason, total)
	})
	if err != nil {
		return OrganizerOrder{}, err
	}
	return loadOrder(ctx, p.pool, `o.id = $1`, orderID)
}

func (p *Postgres) CancelRegistration(ctx context.Context, organizerID, actorID, eventID, orderID string) (OrganizerOrder, error) {
	err := pgx.BeginFunc(ctx, p.pool, func(tx pgx.Tx) error {
		status, total, err := lockEventOrder(ctx, tx, organizerID, eventID, orderID)
		switch {
		case err != nil:
			return err
		case status != "paid":
			return ErrOrderState
		case total > 0:
			return ErrRefundRequired
		}
		if _, err := tx.Exec(ctx, `UPDATE orders SET status = 'cancelled', cancelled_at = now() WHERE id = $1`, orderID); err != nil {
			return err
		}
		if err := invalidateTickets(ctx, tx, orderID, "cancelled"); err != nil {
			return err
		}
		if err := notifyOrderBuyer(ctx, tx, orderID, notice{kind: "registration_cancelled", link: "/me/orders/" + orderID,
			title: "Registration cancelled",
			body:  "The organizer cancelled your free registration. Its tickets are no longer valid."}); err != nil {
			return err
		}
		return writeAudit(ctx, tx, auditEntry{eventID: eventID, actorID: actorID, action: "registration.cancelled",
			entityType: "order", entityID: orderID, details: map[string]any{}})
	})
	if err != nil {
		return OrganizerOrder{}, err
	}
	return loadOrder(ctx, p.pool, `o.id = $1`, orderID)
}

func refundPaidOrder(ctx context.Context, tx pgx.Tx, actorID, eventID, orderID, reason string, total int64) error {
	var paymentID, refundID string
	if err := tx.QueryRow(ctx,
		`UPDATE payments SET status = 'refunded' WHERE order_id = $1 AND status = 'succeeded' RETURNING id`,
		orderID).Scan(&paymentID); err != nil {
		return err
	}
	if err := tx.QueryRow(ctx,
		`INSERT INTO refunds (payment_id, amount, status, reason, provider_refund_id, created_by)
		 VALUES ($1, $2, 'succeeded', NULLIF($3, ''), $4, $5) RETURNING id`,
		paymentID, total, reason, "demo_"+rand.Text(), actorID).Scan(&refundID); err != nil {
		return err
	}
	if _, err := tx.Exec(ctx, `UPDATE orders SET status = 'refunded', refunded_at = now() WHERE id = $1`, orderID); err != nil {
		return err
	}
	if err := invalidateTickets(ctx, tx, orderID, "refunded"); err != nil {
		return err
	}
	if err := notifyOrderBuyer(ctx, tx, orderID, notice{kind: "refund_completed", link: "/me/orders/" + orderID,
		title: "Refund completed",
		body: fmt.Sprintf("Your order was fully refunded (%d KZT, demo refund; no real money moved). "+
			"Its tickets are no longer valid.", total)}); err != nil {
		return err
	}
	return writeAudit(ctx, tx, auditEntry{eventID: eventID, actorID: actorID, action: "refund.succeeded",
		entityType: "refund", entityID: refundID, details: map[string]any{"orderId": orderID, "amount": total, "demo": true}})
}
