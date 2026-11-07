package store

import (
	"context"
	"crypto/rand"
	"errors"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
)

type Payout struct {
	ID                     string     `json:"id"`
	EventID                string     `json:"eventId"`
	EventTitle             string     `json:"eventTitle"`
	OrganizerName          string     `json:"organizerName"`
	GrossAmount            int64      `json:"grossAmount"`
	RefundedAmount         int64      `json:"refundedAmount"`
	FeeAmount              int64      `json:"feeAmount"`
	Amount                 int64      `json:"amount"`
	PayoutAccountReference string     `json:"payoutAccountReference"`
	Status                 string     `json:"status"`
	Demo                   bool       `json:"demo"`
	CreatedAt              time.Time  `json:"createdAt"`
	PaidAt                 *time.Time `json:"paidAt"`
}

const payoutSelect = `SELECT p.id, e.id, e.title, o.name, p.gross_amount, p.refunded_amount, p.fee_amount,
 p.amount, p.payout_account_reference, p.status, p.created_at, p.paid_at
 FROM payouts p JOIN events e ON e.id = p.event_id JOIN organizers o ON o.id = p.organizer_id`

func scanPayout(row pgx.Row) (Payout, error) {
	po := Payout{Demo: true}
	err := row.Scan(&po.ID, &po.EventID, &po.EventTitle, &po.OrganizerName, &po.GrossAmount, &po.RefundedAmount,
		&po.FeeAmount, &po.Amount, &po.PayoutAccountReference, &po.Status, &po.CreatedAt, &po.PaidAt)
	return po, notFound(err)
}

func collectPayouts(ctx context.Context, q querier, sql string, args ...any) ([]Payout, error) {
	rows, err := q.Query(ctx, sql, args...)
	if err != nil {
		return nil, err
	}
	return pgx.CollectRows(rows, func(row pgx.CollectableRow) (Payout, error) { return scanPayout(row) })
}

func (p *Postgres) OrganizerPayouts(ctx context.Context, organizerID string) ([]Payout, error) {
	return collectPayouts(ctx, p.pool, payoutSelect+` WHERE p.organizer_id = $1 ORDER BY p.created_at DESC, p.id`, organizerID)
}

func (p *Postgres) AdminPayouts(ctx context.Context, status string) ([]Payout, error) {
	return collectPayouts(ctx, p.pool, payoutSelect+` WHERE $1 = '' OR p.status = $1
		 ORDER BY p.created_at DESC, p.id LIMIT $2`, status, AdminListLimit)
}

type scheduledPayout struct {
	id          string
	eventID     string
	organizerID string
	status      string
}

var payoutMessages = map[string]string{
	"scheduled": "is scheduled",
	"on_hold":   "is on hold while the platform reviews the event",
	"paid":      "has been marked as paid",
}

func notifyPayout(ctx context.Context, tx pgx.Tx, sp scheduledPayout) error {
	return notifyOrganizer(ctx, tx, sp.organizerID, sp.eventID, notice{kind: "payout_status", link: "/organizer/payouts",
		title: "Payout status: " + strings.ReplaceAll(sp.status, "_", " "),
		body:  "Your demo payout for this event " + payoutMessages[sp.status] + ". Payouts are simulated; no real money moves."})
}

func (p *Postgres) SchedulePayouts(ctx context.Context, actorID string) ([]Payout, error) {
	var created []scheduledPayout
	err := pgx.BeginFunc(ctx, p.pool, func(tx pgx.Tx) error {
		rows, err := tx.Query(ctx,
			`INSERT INTO payouts (event_id, organizer_id, gross_amount, refunded_amount, fee_amount, amount,
			 payout_account_reference, status)
			 SELECT e.id, e.organizer_id, s.gross, s.refunded, s.fees, GREATEST(s.gross - s.refunded - s.fees, 0),
			 o.payout_account_reference,
			 CASE WHEN e.status = 'suspended' OR a.status = 'suspended' OR EXISTS (
			  SELECT 1 FROM disputes d JOIN orders od ON od.id = d.order_id
			  WHERE od.event_id = e.id AND d.status = 'open') THEN 'on_hold' ELSE 'scheduled' END
			 FROM events e JOIN organizers o ON o.id = e.organizer_id
			 JOIN paid_sales_activations a ON a.event_id = e.id AND a.status IN ('active', 'suspended')
			 CROSS JOIN LATERAL (SELECT
			  COALESCE(SUM(total_amount) FILTER (WHERE status IN ('paid','refunded')), 0)::bigint AS gross,
			  COALESCE(SUM(total_amount) FILTER (WHERE status = 'refunded'), 0)::bigint AS refunded,
			  COALESCE(SUM(processing_fee_amount) FILTER (WHERE status = 'paid'), 0)::bigint AS fees
			  FROM orders WHERE event_id = e.id) s
			 WHERE e.ends_at < now()
			 ON CONFLICT (event_id) DO NOTHING
			 RETURNING id, event_id, organizer_id, status`)
		if err != nil {
			return err
		}
		created, err = pgx.CollectRows(rows, func(row pgx.CollectableRow) (scheduledPayout, error) {
			var sp scheduledPayout
			err := row.Scan(&sp.id, &sp.eventID, &sp.organizerID, &sp.status)
			return sp, err
		})
		if err != nil {
			return err
		}
		for _, sp := range created {
			if err := writeAudit(ctx, tx, auditEntry{eventID: sp.eventID, actorID: actorID, action: "payout." + sp.status,
				entityType: "payout", entityID: sp.id, details: map[string]any{"demo": true}}); err != nil {
				return err
			}
			if err := notifyPayout(ctx, tx, sp); err != nil {
				return err
			}
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	ids := make([]string, len(created))
	for i, sp := range created {
		ids[i] = sp.id
	}
	return collectPayouts(ctx, p.pool, payoutSelect+` WHERE p.id = ANY($1::text[]::uuid[]) ORDER BY p.created_at, p.id`, ids)
}

var payoutTransitions = map[string]string{"paid": "scheduled", "on_hold": "scheduled", "scheduled": "on_hold"}

func (p *Postgres) SetPayoutStatus(ctx context.Context, actorID, payoutID, to string) (Payout, error) {
	from, ok := payoutTransitions[to]
	if !ok {
		return Payout{}, ErrStateConflict
	}
	var po Payout
	err := pgx.BeginFunc(ctx, p.pool, func(tx pgx.Tx) error {
		sp := scheduledPayout{id: payoutID, status: to}
		err := tx.QueryRow(ctx,
			`UPDATE payouts SET status = $3, updated_at = now(),
			 paid_at = CASE WHEN $3 = 'paid' THEN now() END,
			 provider_reference = CASE WHEN $3 = 'paid' THEN $4 ELSE provider_reference END
			 WHERE id = $1 AND status = $2 RETURNING event_id, organizer_id`,
			payoutID, from, to, "demo_"+rand.Text()).Scan(&sp.eventID, &sp.organizerID)
		if errors.Is(err, pgx.ErrNoRows) {
			if _, lookupErr := scanPayout(tx.QueryRow(ctx, payoutSelect+` WHERE p.id = $1`, payoutID)); lookupErr != nil {
				return lookupErr
			}
			return ErrStateConflict
		}
		if err != nil {
			return err
		}
		if err := writeAudit(ctx, tx, auditEntry{eventID: sp.eventID, actorID: actorID, action: "payout." + to,
			entityType: "payout", entityID: payoutID, details: map[string]any{"demo": true}}); err != nil {
			return err
		}
		if err := notifyPayout(ctx, tx, sp); err != nil {
			return err
		}
		po, err = scanPayout(tx.QueryRow(ctx, payoutSelect+` WHERE p.id = $1`, payoutID))
		return err
	})
	return po, err
}
