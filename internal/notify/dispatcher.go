package notify

import (
	"bytes"
	"context"
	"log/slog"
	"time"

	"github.com/SWE-2026/biletflow-api/internal/mail"
	"github.com/SWE-2026/biletflow-api/internal/store"
	"github.com/SWE-2026/biletflow-api/internal/tickets"
)

const batchSize = 20

type Mailer interface {
	Send(ctx context.Context, m mail.Message) error
}

type Dispatcher struct {
	DB       *store.Postgres
	Mailer   Mailer
	WebURL   string
	Logger   *slog.Logger
	Interval time.Duration
}

func (d Dispatcher) Run(ctx context.Context) {
	ticker := time.NewTicker(d.Interval)
	defer ticker.Stop()
	for {
		sent, err := d.DB.DispatchEmails(ctx, batchSize, d.send)
		if err != nil && ctx.Err() == nil {
			d.Logger.ErrorContext(ctx, "email dispatch failed", "error", err)
		}
		if sent == batchSize {
			continue
		}
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
	}
}

func (d Dispatcher) send(ctx context.Context, job store.EmailJob) error {
	msg := mail.Message{To: job.To, Subject: job.Title, Text: d.text(job)}
	if job.Kind == store.KindTickets && job.OrderID != nil {
		attachment, err := d.ticketPDF(ctx, *job.OrderID)
		if err != nil {
			return err
		}
		if attachment != nil {
			msg.Attachments = append(msg.Attachments, *attachment)
		}
	}
	if err := d.Mailer.Send(ctx, msg); err != nil {
		d.Logger.WarnContext(ctx, "email not sent", "notification", job.ID, "kind", job.Kind, "error", err)
		return err
	}
	return nil
}

func (d Dispatcher) text(job store.EmailJob) string {
	text := job.Body
	if job.Link != "" {
		text += "\n\nOpen in BiletFlow: " + d.WebURL + job.Link
	}
	return text + "\n\n— BiletFlow\nYou receive this email because of activity on your BiletFlow account."
}

func (d Dispatcher) ticketPDF(ctx context.Context, orderID string) (*mail.Attachment, error) {
	list, err := d.DB.OrderTickets(ctx, orderID)
	if err != nil || len(list) == 0 {
		return nil, err
	}
	var buf bytes.Buffer
	if err := tickets.WritePDF(&buf, list); err != nil {
		return nil, err
	}
	return &mail.Attachment{Name: "biletflow-tickets.pdf", ContentType: "application/pdf", Data: buf.Bytes()}, nil
}
