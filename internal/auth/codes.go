// Package auth holds the account security rules: one-time email codes, password hashing and
// session tokens. It knows nothing about HTTP.
package auth

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"errors"
	"fmt"
	"math/big"
	"time"

	"github.com/SWE-2026/biletflow-api/internal/mail"
	"github.com/SWE-2026/biletflow-api/internal/store"
)

// Code purposes. Each purpose has its own code; only sign-in exists today.
const PurposeSignIn = "sign_in"

const (
	CodeLength  = 6
	CodeTTL     = 10 * time.Minute
	ResendAfter = 30 * time.Second
	MaxAttempts = 5
)

var (
	ErrResendTooSoon   = errors.New("a code was sent recently")
	ErrCodeExpired     = errors.New("code expired or never requested")
	ErrTooManyAttempts = errors.New("too many attempts")
	ErrInvalidCode     = errors.New("code does not match")
)

// CodeStore persists one active code per email and purpose.
type CodeStore interface {
	GetEmailCode(ctx context.Context, email, purpose string) (store.EmailCode, error)
	PutEmailCode(ctx context.Context, c store.EmailCode) error
	ConsumeCodeAttempt(ctx context.Context, email, purpose string, maxAttempts int) (store.EmailCode, error)
	DeleteEmailCode(ctx context.Context, email, purpose string) error
}

type Mailer interface {
	Send(ctx context.Context, m mail.Message) error
}

// Codes issues and verifies one-time email codes.
type Codes struct {
	store  CodeStore
	mailer Mailer
	now    func() time.Time
}

func NewCodes(s CodeStore, m Mailer) *Codes {
	return &Codes{store: s, mailer: m, now: time.Now}
}

// Challenge describes a code that was just sent. Code is the plain value, which callers may expose
// only in local development.
type Challenge struct {
	ExpiresIn time.Duration
	ResendIn  time.Duration
	Code      string
}

// Request generates a code for email and purpose, stores its hash and emails it.
func (c *Codes) Request(ctx context.Context, email, purpose string) (Challenge, error) {
	now := c.now()
	existing, err := c.store.GetEmailCode(ctx, email, purpose)
	switch {
	case err == nil && now.Before(existing.ResendAt):
		return Challenge{ResendIn: existing.ResendAt.Sub(now)}, ErrResendTooSoon
	case err != nil && !errors.Is(err, store.ErrNotFound):
		return Challenge{}, fmt.Errorf("load code: %w", err)
	}
	code, err := generateCode()
	if err != nil {
		return Challenge{}, err
	}
	rec := store.EmailCode{
		Email: email, Purpose: purpose, CodeHash: hashCode(email, purpose, code),
		ExpiresAt: now.Add(CodeTTL), ResendAt: now.Add(ResendAfter),
	}
	if err := c.store.PutEmailCode(ctx, rec); err != nil {
		return Challenge{}, fmt.Errorf("store code: %w", err)
	}
	if err := c.mailer.Send(ctx, codeMessage(email, code)); err != nil {
		return Challenge{}, fmt.Errorf("send code: %w", err)
	}
	return Challenge{ExpiresIn: CodeTTL, ResendIn: ResendAfter, Code: code}, nil
}

// Verify spends one attempt on the code and deletes it once it matches, so each code works once.
func (c *Codes) Verify(ctx context.Context, email, purpose, code string) error {
	rec, err := c.store.ConsumeCodeAttempt(ctx, email, purpose, MaxAttempts)
	switch {
	case errors.Is(err, store.ErrNotFound):
		return ErrCodeExpired
	case errors.Is(err, store.ErrAttemptsExhausted):
		return ErrTooManyAttempts
	case err != nil:
		return fmt.Errorf("consume attempt: %w", err)
	}
	if !c.now().Before(rec.ExpiresAt) {
		_ = c.store.DeleteEmailCode(ctx, email, purpose)
		return ErrCodeExpired
	}
	if subtle.ConstantTimeCompare([]byte(rec.CodeHash), []byte(hashCode(email, purpose, code))) != 1 {
		return ErrInvalidCode
	}
	return c.store.DeleteEmailCode(ctx, email, purpose)
}

func generateCode() (string, error) {
	n, err := rand.Int(rand.Reader, big.NewInt(1_000_000))
	if err != nil {
		return "", err
	}
	return fmt.Sprintf("%0*d", CodeLength, n.Int64()), nil
}

func hashCode(email, purpose, code string) string {
	sum := sha256.Sum256([]byte(purpose + "\x00" + email + "\x00" + code))
	return base64.RawURLEncoding.EncodeToString(sum[:])
}

func codeMessage(email, code string) mail.Message {
	subject := "Your BiletFlow sign-in code"
	body := fmt.Sprintf("Your code is %s. It expires in %d minutes.\n\nIf you did not request it, ignore this email.",
		code, int(CodeTTL.Minutes()))
	return mail.Message{To: email, Subject: subject, Text: body}
}
