package store

import (
	"context"
	"crypto/rand"
	"slices"
	"strings"

	"github.com/jackc/pgx/v5"
)

type CheckoutItem struct {
	TicketTypeID string `json:"ticketTypeId"`
	Quantity     int    `json:"quantity"`
}

type CheckoutInput struct {
	Items         []CheckoutItem `json:"items"`
	AttendeeName  string         `json:"attendeeName"`
	AttendeeEmail string         `json:"attendeeEmail"`
	PromoCode     string         `json:"promoCode"`
	SeatIDs       []string       `json:"seatIds"`
}

type pricedItem struct {
	CheckoutItem
	price int64
}

func (p *Postgres) Checkout(ctx context.Context, userID, eventID string, in CheckoutInput) (Order, error) {
	var orderID string
	err := pgx.BeginFunc(ctx, p.pool, func(tx pgx.Tx) error {
		if _, err := tx.Exec(ctx,
			`UPDATE orders SET status='cancelled', cancelled_at=now()
			 WHERE buyer_id=$1 AND event_id=$2 AND status='pending'`, userID, eventID); err != nil {
			return err
		}
		requested, err := checkoutItems(ctx, tx, eventID, in)
		if err != nil {
			return err
		}
		items, err := reserveItems(ctx, tx, userID, eventID, requested)
		if err != nil {
			return err
		}
		totals := orderTotals{subtotal: subtotal(items)}
		if in.PromoCode != "" {
			if totals.campaignID, totals.discount, err = applyPromo(ctx, tx, eventID, in.PromoCode, items); err != nil {
				return err
			}
		}
		if orderID, err = insertOrder(ctx, tx, userID, eventID, in, items, totals); err != nil {
			return err
		}
		if err := holdSeats(ctx, tx, orderID, in.SeatIDs); err != nil {
			return err
		}
		if totals.subtotal > totals.discount {
			return nil
		}
		if err := issueTickets(ctx, tx, orderID, userID); err != nil {
			return err
		}
		return notifyOrderIssued(ctx, tx, orderID)
	})
	if err != nil {
		return Order{}, err
	}
	o, err := loadOrder(ctx, p.pool, `o.id = $1`, orderID)
	return o.Order, err
}

func checkoutItems(ctx context.Context, tx pgx.Tx, eventID string, in CheckoutInput) ([]CheckoutItem, error) {
	mode, err := eventSeatingMode(ctx, tx, eventID)
	switch {
	case err != nil:
		return nil, err
	case mode == "assigned" && len(in.SeatIDs) == 0:
		return nil, ErrSeatSelectionRequired
	case mode != "assigned" && len(in.SeatIDs) > 0:
		return nil, ErrSeatingNotAssigned
	case mode == "assigned":
		return seatItems(ctx, tx, eventID, in.SeatIDs)
	}
	return in.Items, nil
}

func reserveItems(ctx context.Context, tx pgx.Tx, userID, eventID string, items []CheckoutItem) ([]pricedItem, error) {
	sorted := slices.SortedFunc(slices.Values(items), func(a, b CheckoutItem) int {
		return strings.Compare(a.TicketTypeID, b.TicketTypeID)
	})
	priced := make([]pricedItem, 0, len(sorted))
	paid := false
	for _, item := range sorted {
		s, err := lockForSale(ctx, tx, eventID, item.TicketTypeID)
		if err != nil {
			return nil, err
		}
		if err := checkAvailable(ctx, tx, s, item.TicketTypeID, item.Quantity); err != nil {
			return nil, err
		}
		priced = append(priced, pricedItem{CheckoutItem: item, price: s.price})
		paid = paid || s.price > 0
	}
	if !paid {
		return priced, nil
	}
	active, err := paidSalesActive(ctx, tx, eventID)
	if err == nil && !active {
		err = ErrPaidSalesInactive
	}
	return priced, err
}

func paidSalesActive(ctx context.Context, tx pgx.Tx, eventID string) (bool, error) {
	var active bool
	err := tx.QueryRow(ctx,
		`SELECT EXISTS(SELECT 1 FROM paid_sales_activations WHERE event_id=$1 AND status='active')`,
		eventID).Scan(&active)
	return active, err
}

type orderTotals struct {
	subtotal   int64
	discount   int64
	campaignID string
}

func subtotal(items []pricedItem) int64 {
	var sum int64
	for _, item := range items {
		sum += item.price * int64(item.Quantity)
	}
	return sum
}

func insertOrder(ctx context.Context, tx pgx.Tx, userID, eventID string, in CheckoutInput, items []pricedItem, t orderTotals) (string, error) {
	total := t.subtotal - t.discount
	ids := make([]string, len(items))
	quantities := make([]int32, len(items))
	prices := make([]int64, len(items))
	for i, item := range items {
		ids[i], quantities[i], prices[i] = item.TicketTypeID, int32(item.Quantity), item.price
	}
	settings, err := readSettings(ctx, tx)
	if err != nil {
		return "", err
	}
	status := "pending"
	if total == 0 {
		status = "paid"
	}
	var orderID string
	err = tx.QueryRow(ctx,
		`INSERT INTO orders (status, total_amount, processing_fee_amount, event_id, buyer_id,
		 attendee_name, attendee_email, expires_at, paid_at, subtotal_amount, discount_amount, campaign_id)
		 VALUES ($1, $2, $3, $4, $5, $6, $7,
		 CASE WHEN $1::text = 'pending' THEN now() + make_interval(secs => $8::float8) END,
		 CASE WHEN $1::text = 'paid' THEN now() END, $9, $10, NULLIF($11, '')::uuid) RETURNING id`,
		status, total, ProcessingFee(total, settings.ProcessingFeePercent), eventID, userID, in.AttendeeName, in.AttendeeEmail,
		HoldDuration.Seconds(), t.subtotal, t.discount, t.campaignID).Scan(&orderID)
	if err != nil {
		return "", err
	}
	if _, err := tx.Exec(ctx,
		`INSERT INTO order_items (order_id, ticket_type_id, quantity, unit_price_amount)
		 SELECT $1::uuid, t, q, pr FROM unnest($2::text[]::uuid[], $3::int[], $4::bigint[]) AS u(t, q, pr)`,
		orderID, ids, quantities, prices); err != nil {
		return "", err
	}
	return orderID, nil
}

type issueRow struct {
	itemID   string
	quantity int
	seatIDs  []string
}

func issueTickets(ctx context.Context, tx pgx.Tx, orderID, ownerID string) error {
	rows, err := tx.Query(ctx,
		`SELECT oi.id, oi.quantity,
		 ARRAY(SELECT h.seat_id::text FROM seat_holds h JOIN seats s ON s.id = h.seat_id
		  WHERE h.order_item_id = oi.id ORDER BY s.sort_order)
		 FROM order_items oi WHERE oi.order_id=$1`, orderID)
	if err != nil {
		return err
	}
	items, err := pgx.CollectRows(rows, func(row pgx.CollectableRow) (issueRow, error) {
		var r issueRow
		err := row.Scan(&r.itemID, &r.quantity, &r.seatIDs)
		return r, err
	})
	if err != nil {
		return err
	}
	var itemIDs, tokens, seats []string
	for _, item := range items {
		for i := range item.quantity {
			seat := ""
			if i < len(item.seatIDs) {
				seat = item.seatIDs[i]
			}
			itemIDs = append(itemIDs, item.itemID)
			tokens = append(tokens, rand.Text())
			seats = append(seats, seat)
		}
	}
	_, err = tx.Exec(ctx,
		`INSERT INTO tickets (order_item_id, owner_id, qr_token, seat_id)
		 SELECT i, $2::uuid, t, NULLIF(s, '')::uuid FROM unnest($1::text[]::uuid[], $3::text[], $4::text[]) AS u(i, t, s)`,
		itemIDs, ownerID, tokens, seats)
	return err
}

type pendingOrder struct {
	eventID string
	total   int64
}

func lockPendingOrder(ctx context.Context, tx pgx.Tx, userID, orderID string) (pendingOrder, error) {
	var o pendingOrder
	var status string
	var expired bool
	err := tx.QueryRow(ctx,
		`SELECT event_id, total_amount, status, COALESCE(expires_at <= now(), false)
		 FROM orders WHERE id=$1 AND buyer_id=$2 FOR UPDATE`, orderID, userID).
		Scan(&o.eventID, &o.total, &status, &expired)
	switch {
	case err != nil:
		return o, notFound(err)
	case status != "pending":
		return o, ErrOrderState
	case expired:
		return o, ErrOrderExpired
	}
	return o, nil
}

func checkStillSellable(ctx context.Context, tx pgx.Tx, eventID string) error {
	var published bool
	if err := tx.QueryRow(ctx, `SELECT status = 'published' FROM events WHERE id=$1`, eventID).Scan(&published); err != nil {
		return err
	}
	if !published {
		return ErrInvalidEventState
	}
	active, err := paidSalesActive(ctx, tx, eventID)
	if err == nil && !active {
		err = ErrPaidSalesInactive
	}
	return err
}

func (p *Postgres) PayOrder(ctx context.Context, userID, orderID string, succeed bool) (Order, Payment, error) {
	var payment paymentRow
	err := pgx.BeginFunc(ctx, p.pool, func(tx pgx.Tx) error {
		o, err := lockPendingOrder(ctx, tx, userID, orderID)
		if err != nil {
			return err
		}
		if err := checkStillSellable(ctx, tx, o.eventID); err != nil {
			return err
		}
		status := "failed"
		if succeed {
			status = "succeeded"
		}
		payment, err = scanPayment(tx.QueryRow(ctx,
			`INSERT INTO payments (order_id, amount, status, provider, provider_payment_id)
			 VALUES ($1, $2, $3, $4, $5) RETURNING `+paymentColumns,
			orderID, o.total, status, DemoProvider, "demo_"+rand.Text()))
		if err != nil {
			return err
		}
		if err := writeAudit(ctx, tx, auditEntry{eventID: o.eventID, actorID: userID, action: "payment." + status,
			entityType: "payment", entityID: payment.payment.ID, details: map[string]any{"orderId": orderID, "amount": o.total, "demo": true}}); err != nil {
			return err
		}
		return completePayment(ctx, tx, orderID, userID, succeed)
	})
	if err != nil {
		return Order{}, Payment{}, err
	}
	o, err := loadOrder(ctx, p.pool, `o.id = $1`, orderID)
	return o.Order, payment.payment, err
}

func (p *Postgres) CancelPendingOrder(ctx context.Context, userID, orderID string) (Order, error) {
	tag, err := p.pool.Exec(ctx,
		`UPDATE orders SET status='cancelled', cancelled_at=now()
		 WHERE id=$1 AND buyer_id=$2 AND status='pending'`, orderID, userID)
	if err != nil {
		return Order{}, err
	}
	o, err := loadOrder(ctx, p.pool, `o.id = $1 AND o.buyer_id = $2`, orderID, userID)
	if err == nil && tag.RowsAffected() == 0 {
		err = ErrOrderState
	}
	return o.Order, err
}

func completePayment(ctx context.Context, tx pgx.Tx, orderID, userID string, succeed bool) error {
	if !succeed {
		return notifyOrderBuyer(ctx, tx, orderID, notice{kind: "payment_failed", link: orderLink(orderID),
			title: "Payment failed",
			body: "Your demo payment did not go through, so no tickets were issued. Your tickets stay reserved " +
				"until the hold ends; you can try again from your order."})
	}
	if _, err := tx.Exec(ctx, `UPDATE orders SET status='paid', paid_at=now() WHERE id=$1`, orderID); err != nil {
		return err
	}
	if err := issueTickets(ctx, tx, orderID, userID); err != nil {
		return err
	}
	return notifyOrderIssued(ctx, tx, orderID)
}
