package store

import (
	"context"
	"errors"
	"time"

	"github.com/jackc/pgx/v5"
)

var (
	ErrOrderState        = errors.New("order state does not allow this action")
	ErrOrderExpired      = errors.New("order hold expired")
	ErrPaidSalesInactive = errors.New("paid sales are not active")
	ErrFreeOrder         = errors.New("free orders are cancelled, not refunded")
	ErrRefundRequired    = errors.New("paid orders are refunded, not cancelled")
)

const (
	HoldDuration         = 15 * time.Minute
	ProcessingFeePercent = 3
	DemoProvider         = "demo"
)

func ProcessingFee(total int64) int64 {
	return (total*ProcessingFeePercent + 50) / 100
}

type OrderItem struct {
	TicketTypeID    string `json:"ticketTypeId"`
	TicketTypeName  string `json:"ticketTypeName"`
	Quantity        int    `json:"quantity"`
	UnitPriceAmount int64  `json:"unitPriceAmount"`
}

type Payment struct {
	ID        string    `json:"id"`
	Amount    int64     `json:"amount"`
	Status    string    `json:"status"`
	Demo      bool      `json:"demo"`
	CreatedAt time.Time `json:"createdAt"`
}

type Order struct {
	ID            string      `json:"id"`
	EventID       string      `json:"eventId"`
	EventTitle    string      `json:"eventTitle"`
	Status        string      `json:"status"`
	TotalAmount   int64       `json:"totalAmount"`
	AttendeeName  string      `json:"attendeeName"`
	AttendeeEmail string      `json:"attendeeEmail"`
	ExpiresAt     *time.Time  `json:"expiresAt"`
	PaidAt        *time.Time  `json:"paidAt"`
	CreatedAt     time.Time   `json:"createdAt"`
	Items         []OrderItem `json:"items"`
	Payments      []Payment   `json:"payments"`
}

type OrganizerOrder struct {
	Order
	BuyerEmail          string `json:"buyerEmail"`
	ProcessingFeeAmount int64  `json:"processingFeeAmount"`
}

const orderSelect = `SELECT o.id, o.event_id, e.title,
 CASE WHEN o.status = 'pending' AND o.expires_at <= now() THEN 'expired' ELSE o.status END,
 o.total_amount, o.attendee_name, o.attendee_email, o.expires_at, o.paid_at, o.created_at,
 u.email, o.processing_fee_amount
 FROM orders o JOIN events e ON e.id = o.event_id JOIN users u ON u.id = o.buyer_id
 WHERE `

const orderSort = ` ORDER BY o.created_at DESC, o.id`

func scanOrder(row pgx.CollectableRow) (OrganizerOrder, error) {
	var o OrganizerOrder
	err := row.Scan(&o.ID, &o.EventID, &o.EventTitle, &o.Status, &o.TotalAmount, &o.AttendeeName,
		&o.AttendeeEmail, &o.ExpiresAt, &o.PaidAt, &o.CreatedAt, &o.BuyerEmail, &o.ProcessingFeeAmount)
	o.Items, o.Payments = []OrderItem{}, []Payment{}
	return o, err
}

func loadOrders(ctx context.Context, q querier, where string, args ...any) ([]OrganizerOrder, error) {
	rows, err := q.Query(ctx, orderSelect+where+orderSort, args...)
	if err != nil {
		return nil, err
	}
	orders, err := pgx.CollectRows(rows, scanOrder)
	if err != nil || len(orders) == 0 {
		return orders, err
	}
	index := make(map[string]int, len(orders))
	ids := make([]string, len(orders))
	for i, o := range orders {
		index[o.ID], ids[i] = i, o.ID
	}
	if err := attachItems(ctx, q, orders, index, ids); err != nil {
		return nil, err
	}
	return orders, attachPayments(ctx, q, orders, index, ids)
}

func loadOrder(ctx context.Context, q querier, where string, args ...any) (OrganizerOrder, error) {
	orders, err := loadOrders(ctx, q, where, args...)
	if err != nil {
		return OrganizerOrder{}, err
	}
	if len(orders) == 0 {
		return OrganizerOrder{}, ErrNotFound
	}
	return orders[0], nil
}

type orderItemRow struct {
	orderID string
	item    OrderItem
}

func attachItems(ctx context.Context, q querier, orders []OrganizerOrder, index map[string]int, ids []string) error {
	rows, err := q.Query(ctx,
		`SELECT oi.order_id, oi.ticket_type_id, tt.name, oi.quantity, oi.unit_price_amount
		 FROM order_items oi JOIN ticket_types tt ON tt.id = oi.ticket_type_id
		 WHERE oi.order_id = ANY($1::text[]::uuid[])
		 ORDER BY tt.sort_order, tt.created_at, oi.id`, ids)
	if err != nil {
		return err
	}
	items, err := pgx.CollectRows(rows, func(row pgx.CollectableRow) (orderItemRow, error) {
		var r orderItemRow
		err := row.Scan(&r.orderID, &r.item.TicketTypeID, &r.item.TicketTypeName, &r.item.Quantity, &r.item.UnitPriceAmount)
		return r, err
	})
	for _, r := range items {
		o := &orders[index[r.orderID]]
		o.Items = append(o.Items, r.item)
	}
	return err
}

type paymentRow struct {
	orderID string
	payment Payment
}

func scanPayment(row pgx.Row) (paymentRow, error) {
	var r paymentRow
	var provider string
	err := row.Scan(&r.orderID, &r.payment.ID, &r.payment.Amount, &r.payment.Status, &provider, &r.payment.CreatedAt)
	r.payment.Demo = provider == DemoProvider
	return r, err
}

const paymentColumns = `order_id, id, amount, status, provider, created_at`

func attachPayments(ctx context.Context, q querier, orders []OrganizerOrder, index map[string]int, ids []string) error {
	rows, err := q.Query(ctx,
		`SELECT `+paymentColumns+` FROM payments
		 WHERE order_id = ANY($1::text[]::uuid[]) ORDER BY created_at, id`, ids)
	if err != nil {
		return err
	}
	payments, err := pgx.CollectRows(rows, func(row pgx.CollectableRow) (paymentRow, error) { return scanPayment(row) })
	for _, r := range payments {
		o := &orders[index[r.orderID]]
		o.Payments = append(o.Payments, r.payment)
	}
	return err
}

func (p *Postgres) ListUserOrders(ctx context.Context, userID string) ([]Order, error) {
	orders, err := loadOrders(ctx, p.pool, `o.buyer_id = $1`, userID)
	if err != nil {
		return nil, err
	}
	out := make([]Order, len(orders))
	for i, o := range orders {
		out[i] = o.Order
	}
	return out, nil
}

func (p *Postgres) UserOrder(ctx context.Context, userID, orderID string) (Order, []Ticket, error) {
	o, err := loadOrder(ctx, p.pool, `o.id = $1 AND o.buyer_id = $2`, orderID, userID)
	if err != nil {
		return Order{}, nil, err
	}
	tickets, err := collectTickets(ctx, p.pool, ticketSelect+` AND o.id = $2`+ticketOrder, userID, orderID)
	return o.Order, tickets, err
}

func (p *Postgres) ListEventOrders(ctx context.Context, organizerID, eventID string) ([]OrganizerOrder, error) {
	if _, err := p.EventByOwner(ctx, organizerID, eventID); err != nil {
		return nil, err
	}
	return loadOrders(ctx, p.pool, `o.event_id = $1`, eventID)
}
