// Package mail sends transactional email. Until a provider is chosen, LogMailer stands in for it.
package mail

import (
	"context"
	"log/slog"
)

type Message struct {
	To      string
	Subject string
	Text    string
}

// LogMailer records that an email would have been sent. It logs the subject only: message bodies
// carry one-time codes and must not reach logs.
type LogMailer struct{ Logger *slog.Logger }

func (m LogMailer) Send(ctx context.Context, msg Message) error {
	m.Logger.InfoContext(ctx, "email not sent: no provider configured", "subject", msg.Subject)
	return nil
}
