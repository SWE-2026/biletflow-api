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
	RequestedAt *time.Time         `json:"requestedAt"`
	ActivatedAt *time.Time         `json:"activatedAt"`
	ReviewNote  string             `json:"reviewNote"`
	Checklist   PaidSalesChecklist `json:"checklist"`
}

type rowQuerier interface {
	QueryRow(ctx context.Context, sql string, args ...any) pgx.Row
}

func readPaidSales(ctx context.Context, q rowQuerier, organizerID, eventID string) (PaidSales, error) {
	ps := PaidSales{Demo: true}
	var status, note *string
	var fee *int64
	err := q.QueryRow(ctx,
		`SELECT a.status, a.fee_amount, a.requested_at, a.activated_at, a.review_note,
		 EXISTS(SELECT 1 FROM ticket_types tt WHERE tt.event_id = e.id AND tt.price_amount > 0),
		 o.payout_account_reference <> '', o.contact_phone <> ''
		 FROM events e JOIN organizers o ON o.id = e.organizer_id
		 LEFT JOIN paid_sales_activations a ON a.event_id = e.id
		 WHERE e.id = $1 AND e.organizer_id = $2`, eventID, organizerID).
		Scan(&status, &fee, &ps.RequestedAt, &ps.ActivatedAt, &note, &ps.Checklist.PaidTicketType,
			&ps.Checklist.PayoutAccount, &ps.Checklist.ContactPhone)
	if err != nil {
		return ps, notFound(err)
	}
	settings, err := readSettings(ctx, q)
	if err != nil {
		return ps, err
	}
	ps.Status, ps.FeeAmount = "inactive", settings.ActivationFeeAmount
	if status != nil {
		ps.Status, ps.FeeAmount, ps.ReviewNote = *status, *fee, *note
	}
	return ps, nil
}

func (p *Postgres) PaidSales(ctx context.Context, organizerID, eventID string) (PaidSales, error) {
	return readPaidSales(ctx, p.pool, organizerID, eventID)
}

func (p *Postgres) RequestPaidSales(ctx context.Context, organizerID, actorID, eventID string) (PaidSales, error) {
	err := p.withEditableEvent(ctx, organizerID, eventID, func(tx pgx.Tx) error {
		ps, err := readPaidSales(ctx, tx, organizerID, eventID)
		switch {
		case err != nil:
			return err
		case ps.Status == "active" || ps.Status == "pending":
			return nil
		case ps.Status == "suspended":
			return ErrPaidSalesSuspended
		case !ps.Checklist.complete():
			return ErrActivationIncomplete
		}
		var id string
		var fee int64
		if err := tx.QueryRow(ctx,
			`INSERT INTO paid_sales_activations (event_id, status, requested_at, fee_amount,
			 fee_payment_reference, terms_accepted_at, activated_by)
			 VALUES ($1, 'pending', now(), $2, $3, now(), $4)
			 ON CONFLICT (event_id) DO UPDATE SET status = 'pending', requested_at = now(),
			 review_note = '', terms_accepted_at = now(), activated_by = EXCLUDED.activated_by
			 RETURNING id, fee_amount`, eventID, ps.FeeAmount, "demo_"+rand.Text(), actorID).Scan(&id, &fee); err != nil {
			return err
		}
		return writeAudit(ctx, tx, auditEntry{eventID: eventID, actorID: actorID, action: "paid_sales.requested",
			entityType: "paid_sales_activation", entityID: id,
			details: map[string]any{"feeAmount": fee, "demo": true}})
	})
	if err != nil {
		return PaidSales{}, err
	}
	return readPaidSales(ctx, p.pool, organizerID, eventID)
}

func (p *Postgres) ReviewPaidSales(ctx context.Context, actorID, eventID string, approve bool, note string) error {
	status, action := "rejected", "paid_sales.rejected"
	if approve {
		status, action = "active", "paid_sales.approved"
	}
	return pgx.BeginFunc(ctx, p.pool, func(tx pgx.Tx) error {
		var id string
		err := tx.QueryRow(ctx,
			`UPDATE paid_sales_activations SET status = $2, review_note = $3, reviewed_at = now(),
			 reviewed_by = NULLIF($4, '')::uuid, activated_at = CASE WHEN $2 = 'active' THEN now() END
			 WHERE event_id = $1 AND status = 'pending' RETURNING id`, eventID, status, note, actorID).Scan(&id)
		if errors.Is(err, pgx.ErrNoRows) {
			return ErrStateConflict
		}
		if err != nil {
			return err
		}
		return writeAudit(ctx, tx, auditEntry{eventID: eventID, actorID: actorID, action: action,
			entityType: "paid_sales_activation", entityID: id, details: map[string]any{"note": note}})
	})
}
