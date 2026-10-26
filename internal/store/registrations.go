package store

import (
	"context"
	"crypto/rand"
	"errors"
	"time"

	"github.com/jackc/pgx/v5"
)

var (
	ErrPaymentRequired    = errors.New("ticket type is not free")
	ErrRegistrationClosed = errors.New("registration is closed")
	ErrTicketLimit        = errors.New("ticket limit reached")
	ErrSoldOut            = errors.New("not enough tickets left")
	ErrQuantityBelowSold  = errors.New("quantity below tickets sold")
)

// Ticket is an attendee's ticket with enough event context to show it in a wallet.
type Ticket struct {
	ID             string      `json:"id"`
	Status         string      `json:"status"`
	QRToken        string      `json:"qrToken"`
	CreatedAt      time.Time   `json:"createdAt"`
	OrderID        string      `json:"orderId"`
	TicketTypeID   string      `json:"ticketTypeId"`
	TicketTypeName string      `json:"ticketTypeName"`
	Event          TicketEvent `json:"event"`
}

type TicketEvent struct {
	ID           string    `json:"id"`
	Title        string    `json:"title"`
	Status       string    `json:"status"`
	VenueName    string    `json:"venueName"`
	VenueAddress string    `json:"venueAddress"`
	Timezone     string    `json:"timezone"`
	StartsAt     time.Time `json:"startsAt"`
	EndsAt       time.Time `json:"endsAt"`
}

// SaleWindow holds what decides whether a ticket type can be sold right now.
type SaleWindow struct {
	SalesStartsAt        *time.Time
	SalesEndsAt          *time.Time
	RegistrationOpensAt  *time.Time
	RegistrationClosesAt *time.Time
	EventStartsAt        time.Time
}

// Open reports whether now falls inside both the ticket type's sales window and the event's
// registration window, and before the event starts.
func (w SaleWindow) Open(now time.Time) bool {
	for _, start := range []*time.Time{w.SalesStartsAt, w.RegistrationOpensAt} {
		if start != nil && now.Before(*start) {
			return false
		}
	}
	for _, end := range []*time.Time{w.SalesEndsAt, w.RegistrationClosesAt} {
		if end != nil && !now.Before(*end) {
			return false
		}
	}
	return now.Before(w.EventStartsAt)
}

const ticketSelect = `SELECT t.id, t.status, t.qr_token, t.created_at, o.id, tt.id, tt.name,
 e.id, e.title, e.status, e.venue_name, e.venue_address, e.timezone, e.starts_at, e.ends_at
 FROM tickets t
 JOIN order_items oi ON oi.id = t.order_item_id
 JOIN orders o ON o.id = oi.order_id
 JOIN ticket_types tt ON tt.id = oi.ticket_type_id
 JOIN events e ON e.id = tt.event_id
 WHERE t.owner_id = $1`

const ticketOrder = ` ORDER BY e.starts_at, t.created_at, t.id`

func scanTicket(row pgx.Row) (Ticket, error) {
	var t Ticket
	err := row.Scan(&t.ID, &t.Status, &t.QRToken, &t.CreatedAt, &t.OrderID, &t.TicketTypeID,
		&t.TicketTypeName, &t.Event.ID, &t.Event.Title, &t.Event.Status, &t.Event.VenueName,
		&t.Event.VenueAddress, &t.Event.Timezone, &t.Event.StartsAt, &t.Event.EndsAt)
	return t, err
}

type querier interface {
	Query(ctx context.Context, sql string, args ...any) (pgx.Rows, error)
}

func collectTickets(ctx context.Context, q querier, sql string, args ...any) ([]Ticket, error) {
	rows, err := q.Query(ctx, sql, args...)
	if err != nil {
		return nil, err
	}
	return pgx.CollectRows(rows, func(row pgx.CollectableRow) (Ticket, error) { return scanTicket(row) })
}

func (p *Postgres) ListUserTickets(ctx context.Context, userID string) ([]Ticket, error) {
	return collectTickets(ctx, p.pool, ticketSelect+ticketOrder, userID)
}

// soldTickets counts the ticket type's non-cancelled tickets, in total and for one owner.
func soldTickets(ctx context.Context, tx pgx.Tx, ticketTypeID, ownerID string) (total, owned int, err error) {
	err = tx.QueryRow(ctx,
		`SELECT COUNT(*), COUNT(*) FILTER (WHERE t.owner_id::text = $2)
		 FROM tickets t JOIN order_items oi ON oi.id = t.order_item_id
		 WHERE oi.ticket_type_id = $1 AND t.status <> 'cancelled'`,
		ticketTypeID, ownerID).Scan(&total, &owned)
	return total, owned, err
}

// saleTarget is a ticket type locked for sale, with its event's registration window.
type saleTarget struct {
	price       int64
	quantity    int
	maxPerOrder int
	window      SaleWindow
}

// lockForSale locks a visible ticket type of a published public or unlisted event. Concurrent
// registrations for the same ticket type wait here, so the sold count they read stays accurate.
func lockForSale(ctx context.Context, tx pgx.Tx, eventID, ticketTypeID string) (saleTarget, error) {
	var s saleTarget
	err := tx.QueryRow(ctx,
		`SELECT tt.price_amount, tt.quantity, tt.max_per_order, tt.sales_starts_at, tt.sales_ends_at,
		 e.registration_opens_at, e.registration_closes_at, e.starts_at
		 FROM ticket_types tt JOIN events e ON e.id = tt.event_id
		 WHERE tt.id = $1 AND tt.event_id = $2 AND NOT tt.hidden
		 AND e.status = 'published' AND e.visibility IN ('public','unlisted')
		 FOR UPDATE OF tt`, ticketTypeID, eventID).Scan(&s.price, &s.quantity, &s.maxPerOrder,
		&s.window.SalesStartsAt, &s.window.SalesEndsAt, &s.window.RegistrationOpensAt,
		&s.window.RegistrationClosesAt, &s.window.EventStartsAt)
	return s, notFound(err)
}

// checkSale applies the sale rules in order: free only, window open, per-person limit, stock.
func checkSale(ctx context.Context, tx pgx.Tx, s saleTarget, ticketTypeID, userID string, quantity int) error {
	if s.price > 0 {
		return ErrPaymentRequired
	}
	if !s.window.Open(time.Now()) {
		return ErrRegistrationClosed
	}
	sold, owned, err := soldTickets(ctx, tx, ticketTypeID, userID)
	if err != nil {
		return err
	}
	if owned+quantity > s.maxPerOrder {
		return ErrTicketLimit
	}
	if sold+quantity > s.quantity {
		return ErrSoldOut
	}
	return nil
}

// RegisterFree issues quantity tickets of a free ticket type to userID as one completed order with
// a zero total, and returns the new tickets.
func (p *Postgres) RegisterFree(ctx context.Context, userID, eventID, ticketTypeID string, quantity int) ([]Ticket, error) {
	var tickets []Ticket
	err := pgx.BeginFunc(ctx, p.pool, func(tx pgx.Tx) error {
		s, err := lockForSale(ctx, tx, eventID, ticketTypeID)
		if err != nil {
			return err
		}
		if err := checkSale(ctx, tx, s, ticketTypeID, userID, quantity); err != nil {
			return err
		}
		orderID, err := insertFreeOrder(ctx, tx, userID, eventID, ticketTypeID, quantity)
		if err != nil {
			return err
		}
		tickets, err = collectTickets(ctx, tx, ticketSelect+` AND o.id = $2`+ticketOrder, userID, orderID)
		return err
	})
	return tickets, err
}

func insertFreeOrder(ctx context.Context, tx pgx.Tx, userID, eventID, ticketTypeID string, quantity int) (string, error) {
	var orderID, itemID string
	if err := tx.QueryRow(ctx,
		`INSERT INTO orders (status, total_amount, event_id, buyer_id) VALUES ('paid', 0, $1, $2) RETURNING id`,
		eventID, userID).Scan(&orderID); err != nil {
		return "", err
	}
	if err := tx.QueryRow(ctx,
		`INSERT INTO order_items (order_id, ticket_type_id, quantity, unit_price_amount) VALUES ($1, $2, $3, 0) RETURNING id`,
		orderID, ticketTypeID, quantity).Scan(&itemID); err != nil {
		return "", err
	}
	tokens := make([]string, quantity)
	for i := range tokens {
		tokens[i] = rand.Text()
	}
	_, err := tx.Exec(ctx,
		`INSERT INTO tickets (order_item_id, owner_id, qr_token) SELECT $1, $2, unnest($3::text[])`,
		itemID, userID, tokens)
	return orderID, err
}
