package store

import (
	"context"
	"errors"
	"slices"
	"time"

	"github.com/jackc/pgx/v5"
)

var (
	ErrPromoNotFound      = errors.New("promo code not found")
	ErrPromoDisabled      = errors.New("promo code disabled")
	ErrPromoNotStarted    = errors.New("promo code not active yet")
	ErrPromoExpired       = errors.New("promo code expired")
	ErrPromoExhausted     = errors.New("promo code redemption limit reached")
	ErrPromoNotApplicable = errors.New("promo code does not apply to these tickets")
)

type Promo struct {
	Code          string     `json:"code"`
	Name          string     `json:"name"`
	DiscountType  string     `json:"discountType"`
	DiscountValue int64      `json:"discountValue"`
	TicketTypeIDs []string   `json:"ticketTypeIds"`
	EndsAt        *time.Time `json:"endsAt"`
}

type promoRule struct {
	Promo
	id             string
	status         string
	startsAt       *time.Time
	maxRedemptions *int
	redemptions    int
}

const promoSelect = `SELECT c.id, c.code, c.name, c.discount_type, c.discount_value, c.ends_at,
 c.status, c.starts_at, c.max_redemptions,
 ARRAY(SELECT ticket_type_id::text FROM campaign_ticket_types WHERE campaign_id = c.id ORDER BY 1)
 FROM campaigns c WHERE c.event_id = $1`

func scanPromo(row pgx.Row) (promoRule, error) {
	var r promoRule
	err := row.Scan(&r.id, &r.Code, &r.Name, &r.DiscountType, &r.DiscountValue, &r.EndsAt,
		&r.status, &r.startsAt, &r.maxRedemptions, &r.TicketTypeIDs)
	if errors.Is(err, pgx.ErrNoRows) {
		return r, ErrPromoNotFound
	}
	return r, err
}

func countRedemptions(ctx context.Context, q rowQuerier, campaignID string) (int, error) {
	var n int
	err := q.QueryRow(ctx,
		`SELECT COUNT(*) FROM orders WHERE campaign_id = $1
		 AND (status = 'paid' OR (status = 'pending' AND expires_at > now()))`, campaignID).Scan(&n)
	return n, err
}

func (r promoRule) check(now time.Time) error {
	switch {
	case r.status != "active":
		return ErrPromoDisabled
	case r.startsAt != nil && now.Before(*r.startsAt):
		return ErrPromoNotStarted
	case r.EndsAt != nil && !now.Before(*r.EndsAt):
		return ErrPromoExpired
	case r.maxRedemptions != nil && r.redemptions >= *r.maxRedemptions:
		return ErrPromoExhausted
	}
	return nil
}

func (r promoRule) discount(items []pricedItem) int64 {
	var applicable int64
	for _, item := range items {
		if len(r.TicketTypeIDs) == 0 || slices.Contains(r.TicketTypeIDs, item.TicketTypeID) {
			applicable += item.price * int64(item.Quantity)
		}
	}
	if r.DiscountType == "percent" {
		return (applicable*r.DiscountValue + 50) / 100
	}
	return min(r.DiscountValue, applicable)
}

func (p *Postgres) PublicPromo(ctx context.Context, eventID, code, token string) (Promo, error) {
	if _, err := p.PublicEvent(ctx, eventID); err != nil {
		return Promo{}, err
	}
	match, value := ` AND c.code = $2`, code
	if token != "" {
		match, value = ` AND c.token = $2`, token
	}
	r, err := scanPromo(p.pool.QueryRow(ctx, promoSelect+match, eventID, value))
	if err != nil {
		return Promo{}, err
	}
	if r.redemptions, err = countRedemptions(ctx, p.pool, r.id); err != nil {
		return Promo{}, err
	}
	return r.Promo, r.check(time.Now())
}

func applyPromo(ctx context.Context, tx pgx.Tx, eventID, code string, items []pricedItem) (string, int64, error) {
	r, err := scanPromo(tx.QueryRow(ctx, promoSelect+` AND c.code = $2 FOR UPDATE OF c`, eventID, code))
	if err != nil {
		return "", 0, err
	}
	if r.redemptions, err = countRedemptions(ctx, tx, r.id); err != nil {
		return "", 0, err
	}
	if err := r.check(time.Now()); err != nil {
		return "", 0, err
	}
	discount := r.discount(items)
	if discount == 0 {
		return "", 0, ErrPromoNotApplicable
	}
	return r.id, discount, nil
}
