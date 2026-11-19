package store

import (
	"context"
	"errors"
	"slices"

	"github.com/jackc/pgx/v5"
)

var (
	ErrSeatSelectionRequired = errors.New("this event uses assigned seating")
	ErrSeatingNotAssigned    = errors.New("this event uses general admission")
	ErrSeatUnavailable       = errors.New("a selected seat is not available")
	ErrSeatingLocked         = errors.New("seating cannot change after orders exist")
	ErrSeatingMapping        = errors.New("every seat category needs a ticket type of this event")
)

type LayoutRow struct {
	Label      string `json:"label"`
	Seats      int    `json:"seats"`
	Category   string `json:"category"`
	Accessible []int  `json:"accessible"`
}

type LayoutSection struct {
	Key  string      `json:"key"`
	Rows []LayoutRow `json:"rows"`
}

type SeatingLayout struct {
	Key        string          `json:"key"`
	Categories []string        `json:"categories"`
	Sections   []LayoutSection `json:"sections"`
}

func standardRows(labels string, seats int, category string) []LayoutRow {
	rows := make([]LayoutRow, 0, len(labels))
	for _, label := range labels {
		rows = append(rows, LayoutRow{Label: string(label), Seats: seats, Category: category, Accessible: []int{}})
	}
	return rows
}

var SeatingLayouts = []SeatingLayout{{
	Key:        "hall_a",
	Categories: []string{"vip", "standard", "economy"},
	Sections: []LayoutSection{
		{Key: "stalls", Rows: slices.Concat(
			standardRows("AB", 12, "vip"),
			standardRows("CDEFG", 14, "standard"),
			[]LayoutRow{{Label: "H", Seats: 14, Category: "standard", Accessible: []int{1, 2, 13, 14}}},
		)},
		{Key: "balcony", Rows: standardRows("JKL", 10, "economy")},
	},
}}

func SeatingLayoutByKey(key string) (SeatingLayout, bool) {
	i := slices.IndexFunc(SeatingLayouts, func(l SeatingLayout) bool { return l.Key == key })
	if i < 0 {
		return SeatingLayout{}, false
	}
	return SeatingLayouts[i], true
}

func (l SeatingLayout) seatCounts() map[string]int {
	counts := map[string]int{}
	for _, section := range l.Sections {
		for _, row := range section.Rows {
			counts[row.Category] += row.Seats
		}
	}
	return counts
}

type SeatLabel struct {
	Section string `json:"section"`
	Row     string `json:"row"`
	Number  int    `json:"number"`
}

type Seat struct {
	ID           string  `json:"id"`
	Section      string  `json:"section"`
	Row          string  `json:"row"`
	Number       int     `json:"number"`
	Category     string  `json:"category"`
	Accessible   bool    `json:"accessible"`
	TicketTypeID *string `json:"ticketTypeId"`
	PriceAmount  *int64  `json:"priceAmount"`
	Status       string  `json:"status"`
}

type EventSeating struct {
	Mode   string         `json:"mode"`
	Layout *SeatingLayout `json:"layout"`
	Seats  []Seat         `json:"seats"`
}

type OrganizerSeating struct {
	Mode       string            `json:"mode"`
	LayoutKey  *string           `json:"layoutKey"`
	Categories map[string]string `json:"categories"`
	Locked     bool              `json:"locked"`
}

type SeatingInput struct {
	Mode       string            `json:"mode"`
	LayoutKey  string            `json:"layoutKey"`
	Categories map[string]string `json:"categories"`
}

const seatTaken = `EXISTS (SELECT 1 FROM tickets t WHERE t.seat_id = s.id AND t.status IN ('valid', 'used'))`

const seatHeld = `EXISTS (SELECT 1 FROM seat_holds h JOIN orders o ON o.id = h.order_id
 WHERE h.seat_id = s.id AND o.status = 'pending' AND o.expires_at > now())`

func (p *Postgres) PublicSeating(ctx context.Context, eventID string) (EventSeating, error) {
	e, err := p.PublicEvent(ctx, eventID)
	if err != nil {
		return EventSeating{}, err
	}
	out := EventSeating{Mode: e.SeatingMode, Seats: []Seat{}}
	if e.SeatingLayout == nil {
		return out, nil
	}
	if layout, ok := SeatingLayoutByKey(*e.SeatingLayout); ok {
		out.Layout = &layout
	}
	rows, err := p.pool.Query(ctx,
		`SELECT s.id, s.section, s.row_label, s.number, s.category, s.accessible, tt.id, tt.price_amount,
		 CASE WHEN tt.id IS NULL OR tt.hidden THEN 'unavailable'
		  WHEN `+seatTaken+` THEN 'sold'
		  WHEN `+seatHeld+` THEN 'held'
		  ELSE 'available' END
		 FROM seats s LEFT JOIN ticket_types tt ON tt.id = s.ticket_type_id
		 WHERE s.event_id = $1 ORDER BY s.sort_order`, eventID)
	if err != nil {
		return out, err
	}
	out.Seats, err = pgx.CollectRows(rows, func(row pgx.CollectableRow) (Seat, error) {
		var s Seat
		err := row.Scan(&s.ID, &s.Section, &s.Row, &s.Number, &s.Category, &s.Accessible, &s.TicketTypeID,
			&s.PriceAmount, &s.Status)
		return s, err
	})
	return out, err
}

func (p *Postgres) OrganizerEventSeating(ctx context.Context, organizerID, eventID string) (OrganizerSeating, error) {
	e, err := p.EventByOwner(ctx, organizerID, eventID)
	if err != nil {
		return OrganizerSeating{}, err
	}
	return organizerSeating(ctx, p.pool, e)
}

func organizerSeating(ctx context.Context, q querier, e Event) (OrganizerSeating, error) {
	out := OrganizerSeating{Mode: e.SeatingMode, LayoutKey: e.SeatingLayout, Categories: map[string]string{}}
	rows, err := q.Query(ctx,
		`SELECT DISTINCT category, ticket_type_id::text FROM seats WHERE event_id = $1 AND ticket_type_id IS NOT NULL`, e.ID)
	if err != nil {
		return out, err
	}
	pairs, err := pgx.CollectRows(rows, func(row pgx.CollectableRow) ([2]string, error) {
		var pair [2]string
		err := row.Scan(&pair[0], &pair[1])
		return pair, err
	})
	if err != nil {
		return out, err
	}
	for _, pair := range pairs {
		out.Categories[pair[0]] = pair[1]
	}
	rows, err = q.Query(ctx, `SELECT 1 FROM orders WHERE event_id = $1 LIMIT 1`, e.ID)
	if err != nil {
		return out, err
	}
	hits, err := pgx.CollectRows(rows, pgx.RowTo[int])
	out.Locked = len(hits) > 0
	return out, err
}

func (p *Postgres) ConfigureSeating(ctx context.Context, organizerID, actorID, eventID string, in SeatingInput) (OrganizerSeating, error) {
	var out OrganizerSeating
	err := p.withEditableEvent(ctx, organizerID, eventID, func(tx pgx.Tx) error {
		var hasOrders bool
		if err := tx.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM orders WHERE event_id = $1)`, eventID).Scan(&hasOrders); err != nil {
			return err
		}
		if hasOrders {
			return ErrSeatingLocked
		}
		if _, err := tx.Exec(ctx, `DELETE FROM seats WHERE event_id = $1`, eventID); err != nil {
			return err
		}
		var layoutKey *string
		if in.Mode == "assigned" {
			layout, _ := SeatingLayoutByKey(in.LayoutKey)
			if err := createSeats(ctx, tx, eventID, layout, in.Categories); err != nil {
				return err
			}
			layoutKey = &layout.Key
		}
		e, err := scanEvent(tx.QueryRow(ctx,
			`UPDATE events SET seating_mode = $2, seating_layout = $3, updated_at = now() WHERE id = $1 RETURNING `+eventColumns,
			eventID, in.Mode, layoutKey))
		if err != nil {
			return err
		}
		if err := writeAudit(ctx, tx, auditEntry{eventID: eventID, actorID: actorID, action: "event.seating_configured",
			entityType: "event", entityID: eventID, details: map[string]any{"mode": in.Mode, "layout": layoutKey}}); err != nil {
			return err
		}
		out, err = organizerSeating(ctx, tx, e)
		return err
	})
	return out, err
}

func createSeats(ctx context.Context, tx pgx.Tx, eventID string, layout SeatingLayout, categories map[string]string) error {
	typeIDs := make([]string, 0, len(layout.Categories))
	for _, category := range layout.Categories {
		typeIDs = append(typeIDs, categories[category])
	}
	var owned int
	if err := tx.QueryRow(ctx,
		`SELECT COUNT(DISTINCT id) FROM ticket_types WHERE event_id = $1 AND id::text = ANY($2::text[])`,
		eventID, typeIDs).Scan(&owned); err != nil {
		return err
	}
	if owned != len(slices.Compact(slices.Sorted(slices.Values(typeIDs)))) {
		return ErrSeatingMapping
	}
	var sections, rowLabels, cats, types []string
	var numbers, orders []int32
	var accessible []bool
	for _, section := range layout.Sections {
		for _, row := range section.Rows {
			for n := 1; n <= row.Seats; n++ {
				sections, rowLabels, cats = append(sections, section.Key), append(rowLabels, row.Label), append(cats, row.Category)
				types = append(types, categories[row.Category])
				numbers, orders = append(numbers, int32(n)), append(orders, int32(len(orders)))
				accessible = append(accessible, slices.Contains(row.Accessible, n))
			}
		}
	}
	if _, err := tx.Exec(ctx,
		`INSERT INTO seats (event_id, section, row_label, number, category, accessible, ticket_type_id, sort_order)
		 SELECT $1, s, r, n, c, a, t::uuid, o
		 FROM unnest($2::text[], $3::text[], $4::int[], $5::text[], $6::bool[], $7::text[], $8::int[]) AS u(s, r, n, c, a, t, o)`,
		eventID, sections, rowLabels, numbers, cats, accessible, types, orders); err != nil {
		return err
	}
	return syncSeatQuantities(ctx, tx, eventID)
}

func syncSeatQuantities(ctx context.Context, tx pgx.Tx, eventID string) error {
	if _, err := tx.Exec(ctx,
		`UPDATE ticket_types tt SET quantity = s.n, updated_at = now()
		 FROM (SELECT ticket_type_id, COUNT(*)::int AS n FROM seats WHERE event_id = $1 GROUP BY ticket_type_id) s
		 WHERE tt.id = s.ticket_type_id`, eventID); err != nil {
		return err
	}
	capacity, err := eventCapacity(ctx, tx, eventID)
	if err != nil {
		return err
	}
	return checkCapacity(ctx, tx, eventID, "", capacity, 0)
}

func eventSeatingMode(ctx context.Context, tx pgx.Tx, eventID string) (string, error) {
	var mode string
	err := tx.QueryRow(ctx, `SELECT seating_mode FROM events WHERE id = $1`, eventID).Scan(&mode)
	return mode, notFound(err)
}

type lockedSeat struct {
	id           string
	ticketTypeID string
}

func seatItems(ctx context.Context, tx pgx.Tx, eventID string, seatIDs []string) ([]CheckoutItem, error) {
	ids := slices.Sorted(slices.Values(seatIDs))
	if _, err := tx.Exec(ctx,
		`SELECT id FROM seats WHERE event_id = $1 AND id::text = ANY($2::text[]) ORDER BY id FOR UPDATE`,
		eventID, ids); err != nil {
		return nil, err
	}
	rows, err := tx.Query(ctx,
		`SELECT s.id, s.ticket_type_id::text FROM seats s
		 JOIN ticket_types tt ON tt.id = s.ticket_type_id AND NOT tt.hidden
		 WHERE s.event_id = $1 AND s.id::text = ANY($2::text[]) AND NOT `+seatTaken+` AND NOT `+seatHeld,
		eventID, ids)
	if err != nil {
		return nil, err
	}
	seats, err := pgx.CollectRows(rows, func(row pgx.CollectableRow) (lockedSeat, error) {
		var s lockedSeat
		err := row.Scan(&s.id, &s.ticketTypeID)
		return s, err
	})
	if err != nil {
		return nil, err
	}
	if len(seats) != len(ids) {
		return nil, ErrSeatUnavailable
	}
	counts := map[string]int{}
	for _, s := range seats {
		counts[s.ticketTypeID]++
	}
	items := make([]CheckoutItem, 0, len(counts))
	for typeID, n := range counts {
		items = append(items, CheckoutItem{TicketTypeID: typeID, Quantity: n})
	}
	return items, nil
}

func holdSeats(ctx context.Context, tx pgx.Tx, orderID string, seatIDs []string) error {
	if len(seatIDs) == 0 {
		return nil
	}
	_, err := tx.Exec(ctx,
		`INSERT INTO seat_holds (order_id, seat_id, order_item_id)
		 SELECT $1, s.id, oi.id FROM seats s
		 JOIN order_items oi ON oi.order_id = $1 AND oi.ticket_type_id = s.ticket_type_id
		 WHERE s.id::text = ANY($2::text[])`, orderID, seatIDs)
	return err
}

func copySeating(ctx context.Context, tx pgx.Tx, sourceID, targetID string) error {
	_, err := tx.Exec(ctx,
		`INSERT INTO seats (event_id, section, row_label, number, category, accessible, ticket_type_id, sort_order)
		 SELECT $2, s.section, s.row_label, s.number, s.category, s.accessible,
		 (SELECT nt.id FROM ticket_types ot JOIN ticket_types nt ON nt.event_id = $2 AND nt.name = ot.name
		  AND nt.sort_order = ot.sort_order AND nt.price_amount = ot.price_amount
		  WHERE ot.id = s.ticket_type_id ORDER BY nt.created_at, nt.id LIMIT 1), s.sort_order
		 FROM seats s WHERE s.event_id = $1`, sourceID, targetID)
	return err
}
