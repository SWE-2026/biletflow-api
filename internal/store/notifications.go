package store

import (
	"context"
	"time"

	"github.com/jackc/pgx/v5"
)

const (
	maxNotifications   = 100
	maxEmailAttempts   = 5
	KindOrderConfirmed = "order_confirmed"
	KindTickets        = "tickets_delivered"
)

type Notification struct {
	ID        string    `json:"id"`
	Kind      string    `json:"kind"`
	Title     string    `json:"title"`
	Body      string    `json:"body"`
	Link      string    `json:"link"`
	OrderID   *string   `json:"orderId"`
	EventID   *string   `json:"eventId"`
	Read      bool      `json:"read"`
	CreatedAt time.Time `json:"createdAt"`
}

type notice struct {
	kind  string
	title string
	body  string
	link  string
}

const notificationInsert = `INSERT INTO notifications (user_id, kind, title, body, link, email_to, order_id, event_id) `

func notifyOrderBuyer(ctx context.Context, tx pgx.Tx, orderID string, n notice) error {
	_, err := tx.Exec(ctx, notificationInsert+
		`SELECT o.buyer_id, $2, $3, $4, $5, COALESCE(NULLIF(o.attendee_email, ''), u.email), o.id, o.event_id
		 FROM orders o JOIN users u ON u.id = o.buyer_id WHERE o.id = $1`,
		orderID, n.kind, n.title, n.body, n.link)
	return err
}

func notifyOrderIssued(ctx context.Context, tx pgx.Tx, orderID string) error {
	var title string
	var total int64
	if err := tx.QueryRow(ctx,
		`SELECT e.title, o.total_amount FROM orders o JOIN events e ON e.id = o.event_id WHERE o.id = $1`,
		orderID).Scan(&title, &total); err != nil {
		return err
	}
	link := "/me/orders/" + orderID
	confirm := notice{kind: KindOrderConfirmed, link: link,
		title: "Registration confirmed: " + title,
		body:  "Your registration for " + title + " is confirmed. No payment was needed."}
	if total > 0 {
		confirm.title = "Purchase confirmed: " + title
		confirm.body = "Your demo payment for " + title + " succeeded and your order is confirmed. " +
			"This is a simulated payment; no real money was charged."
	}
	if err := notifyOrderBuyer(ctx, tx, orderID, confirm); err != nil {
		return err
	}
	return notifyOrderBuyer(ctx, tx, orderID, notice{kind: KindTickets, link: link,
		title: "Your tickets for " + title,
		body:  "Your QR-code tickets for " + title + " are attached as a printable PDF and are always available in your account."})
}

func notifyTicketHolders(ctx context.Context, tx pgx.Tx, eventID string, n notice) error {
	_, err := tx.Exec(ctx, notificationInsert+
		`SELECT o.buyer_id, $2, $3, $4, $5, COALESCE(NULLIF(o.attendee_email, ''), u.email), o.id, o.event_id
		 FROM orders o JOIN users u ON u.id = o.buyer_id
		 WHERE o.event_id = $1 AND o.status = 'paid'`,
		eventID, n.kind, n.title, n.body, n.link)
	return err
}

func notifyOrganizer(ctx context.Context, tx pgx.Tx, organizerID, eventID string, n notice) error {
	_, err := tx.Exec(ctx, notificationInsert+
		`SELECT u.id, $2, $3, $4, $5, u.email, NULL, NULLIF($6, '')::uuid
		 FROM users u WHERE u.organizer_id = $1 AND u.suspended_at IS NULL`,
		organizerID, n.kind, n.title, n.body, n.link, eventID)
	return err
}

func notifyUser(ctx context.Context, tx pgx.Tx, userID, eventID string, n notice) error {
	_, err := tx.Exec(ctx, notificationInsert+
		`SELECT u.id, $2, $3, $4, $5, u.email, NULL, NULLIF($6, '')::uuid FROM users u WHERE u.id = $1`,
		userID, n.kind, n.title, n.body, n.link, eventID)
	return err
}

func notifyAdmins(ctx context.Context, tx pgx.Tx, n notice) error {
	_, err := tx.Exec(ctx, notificationInsert+
		`SELECT u.id, $1, $2, $3, $4, u.email, NULL, NULL FROM users u
		 WHERE u.role = 'admin' AND u.suspended_at IS NULL`,
		n.kind, n.title, n.body, n.link)
	return err
}

func (p *Postgres) ListNotifications(ctx context.Context, userID string, unreadOnly bool) ([]Notification, int, error) {
	rows, err := p.pool.Query(ctx,
		`SELECT id, kind, title, body, link, order_id, event_id, read_at IS NOT NULL, created_at
		 FROM notifications WHERE user_id = $1 AND (NOT $2 OR read_at IS NULL)
		 ORDER BY created_at DESC, id LIMIT $3`, userID, unreadOnly, maxNotifications)
	if err != nil {
		return nil, 0, err
	}
	list, err := pgx.CollectRows(rows, func(row pgx.CollectableRow) (Notification, error) {
		var n Notification
		err := row.Scan(&n.ID, &n.Kind, &n.Title, &n.Body, &n.Link, &n.OrderID, &n.EventID, &n.Read, &n.CreatedAt)
		return n, err
	})
	if err != nil {
		return nil, 0, err
	}
	var unread int
	err = p.pool.QueryRow(ctx, `SELECT COUNT(*) FROM notifications WHERE user_id = $1 AND read_at IS NULL`, userID).Scan(&unread)
	return list, unread, err
}

func (p *Postgres) MarkNotificationsRead(ctx context.Context, userID, id string) error {
	tag, err := p.pool.Exec(ctx,
		`UPDATE notifications SET read_at = COALESCE(read_at, now())
		 WHERE user_id = $1 AND ($2 = '' OR id::text = $2)`, userID, id)
	if err == nil && id != "" && tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	return err
}

type EmailJob struct {
	ID      string
	Kind    string
	To      string
	Title   string
	Body    string
	Link    string
	OrderID *string
}

func (p *Postgres) DispatchEmails(ctx context.Context, limit int, send func(context.Context, EmailJob) error) (int, error) {
	sent := 0
	err := pgx.BeginFunc(ctx, p.pool, func(tx pgx.Tx) error {
		rows, err := tx.Query(ctx,
			`SELECT id, kind, email_to, title, body, link, order_id FROM notifications
			 WHERE email_status = 'pending' ORDER BY created_at, id LIMIT $1 FOR UPDATE SKIP LOCKED`, limit)
		if err != nil {
			return err
		}
		jobs, err := pgx.CollectRows(rows, func(row pgx.CollectableRow) (EmailJob, error) {
			var j EmailJob
			err := row.Scan(&j.ID, &j.Kind, &j.To, &j.Title, &j.Body, &j.Link, &j.OrderID)
			return j, err
		})
		if err != nil {
			return err
		}
		for _, j := range jobs {
			sendErr := send(ctx, j)
			if sendErr == nil {
				sent++
			}
			if _, err := tx.Exec(ctx,
				`UPDATE notifications SET email_attempts = email_attempts + 1,
				 email_status = CASE WHEN $2 THEN 'sent' WHEN email_attempts + 1 >= $3 THEN 'failed' ELSE 'pending' END,
				 emailed_at = CASE WHEN $2 THEN now() END
				 WHERE id = $1`, j.ID, sendErr == nil, maxEmailAttempts); err != nil {
				return err
			}
		}
		return nil
	})
	return sent, err
}
