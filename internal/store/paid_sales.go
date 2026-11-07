package store

import (
	"context"
	"crypto/rand"
	"errors"
	"time"

	"github.com/jackc/pgx/v5"
)

var (
	ErrPaidSalesSuspended   = errors.New("paid sales suspended")
	ErrActivationIncomplete = errors.New("paid sales checklist incomplete")
)

const PaidSalesActivationFee int64 = 10000

type PaidSalesChecklist struct {
	PaidTicketType bool `json:"paidTicketType"`
	PayoutAccount  bool `json:"payoutAccount"`
	ContactPhone   bool `json:"contactPhone"`
}

func (c PaidSalesChecklist) complete() bool {
	return c.PaidTicketType && c.PayoutAccount && c.ContactPhone
}

type PaidSales struct {
	Status      string             `json:"status"`
	FeeAmount   int64              `json:"feeAmount"`
	Demo        bool               `json:"demo"`
	ActivatedAt *time.Time         `json:"activatedAt"`
	Checklist   PaidSalesChecklist `json:"checklist"`
}

type rowQuerier interface {
	QueryRow(ctx context.Context, sql string, args ...any) pgx.Row
}

func readPaidSales(ctx context.Context, q rowQuerier, organizerID, eventID string) (PaidSales, error) {
	ps := PaidSales{Demo: true}
	var status *string
	var fee *int64
	err := q.QueryRow(ctx,
		`SELECT a.status, a.fee_amount, a.activated_at,
		 EXISTS(SELECT 1 FROM ticket_types tt WHERE tt.event_id = e.id AND tt.price_amount > 0),
		 o.payout_account_reference <> '', o.contact_phone <> ''
		 FROM events e JOIN organizers o ON o.id = e.organizer_id
		 LEFT JOIN paid_sales_activations a ON a.event_id = e.id
		 WHERE e.id = $1 AND e.organizer_id = $2`, eventID, organizerID).
		Scan(&status, &fee, &ps.ActivatedAt, &ps.Checklist.PaidTicketType,
			&ps.Checklist.PayoutAccount, &ps.Checklist.ContactPhone)
	if err != nil {
		return ps, notFound(err)
	}
	ps.Status, ps.FeeAmount = "inactive", PaidSalesActivationFee
	if status != nil && (*status == "active" || *status == "suspended") {
		ps.Status, ps.FeeAmount = *status, *fee
	}
	return ps, nil
}

func (p *Postgres) PaidSales(ctx context.Context, organizerID, eventID string) (PaidSales, error) {
	return readPaidSales(ctx, p.pool, organizerID, eventID)
}

func (p *Postgres) ActivatePaidSales(ctx context.Context, organizerID, actorID, eventID string) (PaidSales, error) {
	err := p.withEditableEvent(ctx, organizerID, eventID, func(tx pgx.Tx) error {
		ps, err := readPaidSales(ctx, tx, organizerID, eventID)
		switch {
		case err != nil:
			return err
		case ps.Status == "active":
			return nil
		case ps.Status == "suspended":
			return ErrPaidSalesSuspended
		case !ps.Checklist.complete():
			return ErrActivationIncomplete
		}
		var id string
		if err := tx.QueryRow(ctx,
			`INSERT INTO paid_sales_activations (event_id, status, activated_at, fee_amount,
			 fee_payment_reference, terms_accepted_at, activated_by)
			 VALUES ($1, 'active', now(), $2, $3, now(), $4)
			 ON CONFLICT (event_id) DO UPDATE SET status='active', activated_at=now(),
			 fee_amount=EXCLUDED.fee_amount, fee_payment_reference=EXCLUDED.fee_payment_reference,
			 terms_accepted_at=now(), activated_by=EXCLUDED.activated_by
			 RETURNING id`, eventID, PaidSalesActivationFee, "demo_"+rand.Text(), actorID).Scan(&id); err != nil {
			return err
		}
		return writeAudit(ctx, tx, auditEntry{eventID: eventID, actorID: actorID, action: "paid_sales.activated",
			entityType: "paid_sales_activation", entityID: id,
			details: map[string]any{"feeAmount": PaidSalesActivationFee, "demo": true}})
	})
	if err != nil {
		return PaidSales{}, err
	}
	return readPaidSales(ctx, p.pool, organizerID, eventID)
}
