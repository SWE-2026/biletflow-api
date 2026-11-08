package store

import (
	"context"
	"strconv"
	"time"

	"github.com/jackc/pgx/v5"
)

const (
	AdminListLimit = 200
	ExportLimit    = 10000
)

type AdminUser struct {
	ID            string     `json:"id"`
	Email         string     `json:"email"`
	DisplayName   string     `json:"displayName"`
	Role          string     `json:"role"`
	OrganizerID   *string    `json:"organizerId"`
	EmailVerified bool       `json:"emailVerified"`
	SuspendedAt   *time.Time `json:"suspendedAt"`
	CreatedAt     time.Time  `json:"createdAt"`
}

type AdminPayment struct {
	ID         string    `json:"id"`
	OrderID    string    `json:"orderId"`
	EventID    string    `json:"eventId"`
	EventTitle string    `json:"eventTitle"`
	BuyerEmail string    `json:"buyerEmail"`
	Amount     int64     `json:"amount"`
	Status     string    `json:"status"`
	Demo       bool      `json:"demo"`
	CreatedAt  time.Time `json:"createdAt"`
}

type AdminRefund struct {
	ID         string    `json:"id"`
	OrderID    string    `json:"orderId"`
	EventID    string    `json:"eventId"`
	EventTitle string    `json:"eventTitle"`
	Amount     int64     `json:"amount"`
	Status     string    `json:"status"`
	Reason     *string   `json:"reason"`
	CreatedBy  *string   `json:"createdBy"`
	Demo       bool      `json:"demo"`
	CreatedAt  time.Time `json:"createdAt"`
}

// AdminActivation is a paid sales request with what an admin needs to review it.
type AdminActivation struct {
	EventID                string     `json:"eventId"`
	EventTitle             string     `json:"eventTitle"`
	OrganizerName          string     `json:"organizerName"`
	ContactEmail           string     `json:"contactEmail"`
	ContactPhone           string     `json:"contactPhone"`
	PayoutAccountReference string     `json:"payoutAccountReference"`
	Status                 string     `json:"status"`
	FeeAmount              int64      `json:"feeAmount"`
	Demo                   bool       `json:"demo"`
	RequestedAt            *time.Time `json:"requestedAt"`
	ActivatedAt            *time.Time `json:"activatedAt"`
	SuspendedAt            *time.Time `json:"suspendedAt"`
	ReviewNote             string     `json:"reviewNote"`
}

func limitClause(limit int) string { return " LIMIT " + strconv.Itoa(limit) }

func (p *Postgres) AdminUsers(ctx context.Context, query string) ([]AdminUser, error) {
	rows, err := p.pool.Query(ctx,
		`SELECT id, email, display_name, role, organizer_id, email_verified_at IS NOT NULL, suspended_at, created_at
		 FROM users WHERE $1 = '' OR email ILIKE $2 OR display_name ILIKE $2 OR id::text = $1
		 ORDER BY created_at DESC, id LIMIT $3`, query, likePattern(query), AdminListLimit)
	if err != nil {
		return nil, err
	}
	return pgx.CollectRows(rows, func(row pgx.CollectableRow) (AdminUser, error) {
		var u AdminUser
		err := row.Scan(&u.ID, &u.Email, &u.DisplayName, &u.Role, &u.OrganizerID, &u.EmailVerified, &u.SuspendedAt, &u.CreatedAt)
		return u, err
	})
}

func (p *Postgres) AdminEvents(ctx context.Context, query, status string) ([]Event, error) {
	rows, err := p.pool.Query(ctx,
		`SELECT `+eventColumns+` FROM events
		 WHERE ($1 = '' OR title ILIKE $2 OR venue_name ILIKE $2 OR id::text = $1)
		 AND ($3 = '' OR status = $3)
		 ORDER BY created_at DESC, id LIMIT $4`, query, likePattern(query), status, AdminListLimit)
	if err != nil {
		return nil, err
	}
	return pgx.CollectRows(rows, func(row pgx.CollectableRow) (Event, error) { return scanEvent(row) })
}

func (p *Postgres) AdminEvent(ctx context.Context, id string) (Event, error) {
	return scanEvent(p.pool.QueryRow(ctx, `SELECT `+eventColumns+` FROM events WHERE id = $1`, id))
}

func (p *Postgres) AdminOrders(ctx context.Context, query, status string, limit int) ([]OrganizerOrder, error) {
	return loadOrdersLimit(ctx, p.pool,
		`($1 = '' OR o.id::text = $1 OR u.email ILIKE $2 OR o.attendee_email ILIKE $2 OR e.title ILIKE $2)
		 AND ($3 = '' OR o.status = $3)`, limitClause(limit), query, likePattern(query), status)
}

func (p *Postgres) AdminPayments(ctx context.Context, query, status string, limit int) ([]AdminPayment, error) {
	rows, err := p.pool.Query(ctx,
		`SELECT pm.id, o.id, e.id, e.title, u.email, pm.amount, pm.status, pm.provider, pm.created_at
		 FROM payments pm JOIN orders o ON o.id = pm.order_id JOIN events e ON e.id = o.event_id
		 JOIN users u ON u.id = o.buyer_id
		 WHERE ($1 = '' OR o.id::text = $1 OR u.email ILIKE $2 OR e.title ILIKE $2)
		 AND ($3 = '' OR pm.status = $3)
		 ORDER BY pm.created_at DESC, pm.id LIMIT $4`, query, likePattern(query), status, limit)
	if err != nil {
		return nil, err
	}
	return pgx.CollectRows(rows, func(row pgx.CollectableRow) (AdminPayment, error) {
		var pm AdminPayment
		var provider string
		err := row.Scan(&pm.ID, &pm.OrderID, &pm.EventID, &pm.EventTitle, &pm.BuyerEmail, &pm.Amount,
			&pm.Status, &provider, &pm.CreatedAt)
		pm.Demo = provider == DemoProvider
		return pm, err
	})
}

func (p *Postgres) AdminRefunds(ctx context.Context, limit int) ([]AdminRefund, error) {
	rows, err := p.pool.Query(ctx,
		`SELECT r.id, o.id, e.id, e.title, r.amount, r.status, r.reason, cu.email, pm.provider, r.created_at
		 FROM refunds r JOIN payments pm ON pm.id = r.payment_id JOIN orders o ON o.id = pm.order_id
		 JOIN events e ON e.id = o.event_id LEFT JOIN users cu ON cu.id = r.created_by
		 ORDER BY r.created_at DESC, r.id LIMIT $1`, limit)
	if err != nil {
		return nil, err
	}
	return pgx.CollectRows(rows, func(row pgx.CollectableRow) (AdminRefund, error) {
		var r AdminRefund
		var provider string
		err := row.Scan(&r.ID, &r.OrderID, &r.EventID, &r.EventTitle, &r.Amount, &r.Status, &r.Reason,
			&r.CreatedBy, &provider, &r.CreatedAt)
		r.Demo = provider == DemoProvider
		return r, err
	})
}

func (p *Postgres) AdminActivations(ctx context.Context, status string) ([]AdminActivation, error) {
	rows, err := p.pool.Query(ctx,
		`SELECT e.id, e.title, o.name, o.contact_email, o.contact_phone, o.payout_account_reference,
		 a.status, a.fee_amount, a.requested_at, a.activated_at, a.suspended_at, a.review_note
		 FROM paid_sales_activations a JOIN events e ON e.id = a.event_id JOIN organizers o ON o.id = e.organizer_id
		 WHERE $1 = '' OR a.status = $1
		 ORDER BY a.requested_at DESC NULLS LAST, e.id LIMIT $2`, status, AdminListLimit)
	if err != nil {
		return nil, err
	}
	return pgx.CollectRows(rows, func(row pgx.CollectableRow) (AdminActivation, error) {
		a := AdminActivation{Demo: true}
		err := row.Scan(&a.EventID, &a.EventTitle, &a.OrganizerName, &a.ContactEmail, &a.ContactPhone,
			&a.PayoutAccountReference, &a.Status, &a.FeeAmount, &a.RequestedAt, &a.ActivatedAt, &a.SuspendedAt,
			&a.ReviewNote)
		return a, err
	})
}

func (p *Postgres) AdminCampaigns(ctx context.Context) ([]Campaign, error) {
	rows, err := p.pool.Query(ctx, campaignFrom+` ORDER BY c.created_at DESC, c.id LIMIT $1`, AdminListLimit)
	if err != nil {
		return nil, err
	}
	return pgx.CollectRows(rows, func(row pgx.CollectableRow) (Campaign, error) { return scanCampaign(row) })
}

func (p *Postgres) AdminActivity(ctx context.Context, eventID string, f ActivityFilter) ([]ActivityEntry, error) {
	if _, err := p.AdminEvent(ctx, eventID); err != nil {
		return nil, err
	}
	return p.listActivity(ctx, eventID, f)
}
