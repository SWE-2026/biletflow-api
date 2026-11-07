package store

import (
	"context"
	"math"
	"time"

	"github.com/jackc/pgx/v5"
)

type AnalyticsFilter struct {
	From         *time.Time
	To           *time.Time
	TicketTypeID string
}

type TicketTypeStats struct {
	TicketTypeID string `json:"ticketTypeId"`
	Name         string `json:"name"`
	PriceAmount  int64  `json:"priceAmount"`
	Quantity     int    `json:"quantity"`
	Available    int    `json:"available"`
	Reserved     int    `json:"reserved"`
	Sold         int    `json:"sold"`
	Refunded     int    `json:"refunded"`
	Cancelled    int    `json:"cancelled"`
	CheckedIn    int    `json:"checkedIn"`
	GrossAmount  int64  `json:"grossAmount"`
}

type SalesDay struct {
	Date        string `json:"date"`
	Orders      int    `json:"orders"`
	Tickets     int    `json:"tickets"`
	GrossAmount int64  `json:"grossAmount"`
}

type Analytics struct {
	Capacity         int               `json:"capacity"`
	TicketsSold      int               `json:"ticketsSold"`
	TicketsReserved  int               `json:"ticketsReserved"`
	TicketsRemaining int               `json:"ticketsRemaining"`
	PercentSold      float64           `json:"percentSold"`
	GrossAmount      int64             `json:"grossAmount"`
	DiscountAmount   int64             `json:"discountAmount"`
	RefundedAmount   int64             `json:"refundedAmount"`
	NetAmount        int64             `json:"netAmount"`
	Orders           int               `json:"orders"`
	CheckedIn        int               `json:"checkedIn"`
	Absent           int               `json:"absent"`
	CheckInPercent   float64           `json:"checkInPercent"`
	Demo             bool              `json:"demo"`
	TicketTypes      []TicketTypeStats `json:"ticketTypes"`
	SalesOverTime    []SalesDay        `json:"salesOverTime"`
	Campaigns        []Campaign        `json:"campaigns"`
}

func percent(part, whole int) float64 {
	if whole == 0 {
		return 0
	}
	return math.Round(float64(part)*1000/float64(whole)) / 10
}

func (p *Postgres) EventAnalytics(ctx context.Context, organizerID, eventID string, f AnalyticsFilter) (Analytics, error) {
	a := Analytics{Demo: true}
	e, err := p.EventByOwner(ctx, organizerID, eventID)
	if err != nil {
		return a, err
	}
	if a.TicketTypes, err = p.ticketTypeStats(ctx, eventID, f); err != nil {
		return a, err
	}
	a.Capacity = e.Capacity
	if f.TicketTypeID != "" {
		a.Capacity = 0
	}
	for _, t := range a.TicketTypes {
		a.TicketsSold += t.Sold
		a.TicketsReserved += t.Reserved
		a.CheckedIn += t.CheckedIn
		if f.TicketTypeID != "" {
			a.Capacity += t.Quantity
		}
	}
	a.TicketsRemaining = max(a.Capacity-a.TicketsSold, 0)
	a.PercentSold = percent(a.TicketsSold, a.Capacity)
	a.Absent = a.TicketsSold - a.CheckedIn
	a.CheckInPercent = percent(a.CheckedIn, a.TicketsSold)
	if err := p.pool.QueryRow(ctx,
		`SELECT COALESCE(SUM(subtotal_amount), 0)::bigint, COALESCE(SUM(discount_amount), 0)::bigint,
		 COALESCE(SUM(total_amount) FILTER (WHERE status = 'refunded'), 0)::bigint, COUNT(*)
		 FROM orders WHERE event_id = $1 AND status IN ('paid','refunded')
		 AND ($2::timestamptz IS NULL OR paid_at >= $2) AND ($3::timestamptz IS NULL OR paid_at < $3)`,
		eventID, f.From, f.To).Scan(&a.GrossAmount, &a.DiscountAmount, &a.RefundedAmount, &a.Orders); err != nil {
		return a, err
	}
	a.NetAmount = a.GrossAmount - a.DiscountAmount - a.RefundedAmount
	if a.SalesOverTime, err = p.salesOverTime(ctx, eventID, f); err != nil {
		return a, err
	}
	a.Campaigns, err = p.ListCampaigns(ctx, organizerID, eventID)
	return a, err
}

func (p *Postgres) ticketTypeStats(ctx context.Context, eventID string, f AnalyticsFilter) ([]TicketTypeStats, error) {
	rows, err := p.pool.Query(ctx,
		`SELECT tt.id, tt.name, tt.price_amount, tt.quantity,
		 COUNT(t.id) FILTER (WHERE t.status IN ('valid','used')),
		 COUNT(t.id) FILTER (WHERE t.status = 'refunded'),
		 COUNT(t.id) FILTER (WHERE t.status = 'cancelled'),
		 COUNT(t.id) FILTER (WHERE t.status = 'used'),
		 COALESCE((SELECT SUM(h.quantity) FROM order_items h JOIN orders ho ON ho.id = h.order_id
		  WHERE h.ticket_type_id = tt.id AND ho.status = 'pending' AND ho.expires_at > now()), 0)::bigint,
		 COALESCE((SELECT SUM(s.quantity * s.unit_price_amount) FROM order_items s JOIN orders so ON so.id = s.order_id
		  WHERE s.ticket_type_id = tt.id AND so.status IN ('paid','refunded')
		  AND ($3::timestamptz IS NULL OR so.paid_at >= $3) AND ($4::timestamptz IS NULL OR so.paid_at < $4)), 0)::bigint
		 FROM ticket_types tt
		 LEFT JOIN order_items oi ON oi.ticket_type_id = tt.id
		 LEFT JOIN tickets t ON t.order_item_id = oi.id
		 WHERE tt.event_id = $1 AND ($2 = '' OR tt.id::text = $2)
		 GROUP BY tt.id ORDER BY tt.sort_order, tt.created_at, tt.id`, eventID, f.TicketTypeID, f.From, f.To)
	if err != nil {
		return nil, err
	}
	stats, err := pgx.CollectRows(rows, func(row pgx.CollectableRow) (TicketTypeStats, error) {
		var s TicketTypeStats
		err := row.Scan(&s.TicketTypeID, &s.Name, &s.PriceAmount, &s.Quantity, &s.Sold, &s.Refunded,
			&s.Cancelled, &s.CheckedIn, &s.Reserved, &s.GrossAmount)
		s.Available = max(s.Quantity-s.Sold-s.Reserved, 0)
		return s, err
	})
	if stats == nil {
		stats = []TicketTypeStats{}
	}
	return stats, err
}

func (p *Postgres) salesOverTime(ctx context.Context, eventID string, f AnalyticsFilter) ([]SalesDay, error) {
	rows, err := p.pool.Query(ctx,
		`SELECT to_char(o.paid_at AT TIME ZONE e.timezone, 'YYYY-MM-DD') AS day, COUNT(DISTINCT o.id),
		 SUM(oi.quantity)::bigint, SUM(oi.quantity * oi.unit_price_amount)::bigint
		 FROM orders o JOIN events e ON e.id = o.event_id JOIN order_items oi ON oi.order_id = o.id
		 WHERE o.event_id = $1 AND o.status IN ('paid','refunded')
		 AND ($2::timestamptz IS NULL OR o.paid_at >= $2) AND ($3::timestamptz IS NULL OR o.paid_at < $3)
		 AND ($4 = '' OR oi.ticket_type_id::text = $4)
		 GROUP BY day ORDER BY day`, eventID, f.From, f.To, f.TicketTypeID)
	if err != nil {
		return nil, err
	}
	days, err := pgx.CollectRows(rows, func(row pgx.CollectableRow) (SalesDay, error) {
		var d SalesDay
		err := row.Scan(&d.Date, &d.Orders, &d.Tickets, &d.GrossAmount)
		return d, err
	})
	if days == nil {
		days = []SalesDay{}
	}
	return days, err
}
