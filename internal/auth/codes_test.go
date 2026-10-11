package auth

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/SWE-2026/biletflow-api/internal/mail"
	"github.com/SWE-2026/biletflow-api/internal/store"
)

type fakeCodeStore struct{ codes map[string]store.EmailCode }

func newFakeCodeStore() *fakeCodeStore { return &fakeCodeStore{codes: map[string]store.EmailCode{}} }

func (f *fakeCodeStore) GetEmailCode(_ context.Context, email, purpose string) (store.EmailCode, error) {
	c, ok := f.codes[email+purpose]
	if !ok {
		return c, store.ErrNotFound
	}
	return c, nil
}

func (f *fakeCodeStore) PutEmailCode(_ context.Context, c store.EmailCode) error {
	f.codes[c.Email+c.Purpose] = c
	return nil
}

func (f *fakeCodeStore) ConsumeCodeAttempt(_ context.Context, email, purpose string, maxAttempts int) (store.EmailCode, error) {
	c, ok := f.codes[email+purpose]
	if !ok {
		return c, store.ErrNotFound
	}
	if c.Attempts >= maxAttempts {
		return c, store.ErrAttemptsExhausted
	}
	c.Attempts++
	f.codes[email+purpose] = c
	return c, nil
}

func (f *fakeCodeStore) DeleteEmailCode(_ context.Context, email, purpose string) error {
	delete(f.codes, email+purpose)
	return nil
}

type fakeMailer struct{ sent []mail.Message }

func (f *fakeMailer) Send(_ context.Context, m mail.Message) error {
	f.sent = append(f.sent, m)
	return nil
}

type fixture struct {
	codes  *Codes
	mailer *fakeMailer
	clock  *time.Time
}

func newFixture() fixture {
	now := time.Date(2026, 10, 11, 12, 0, 0, 0, time.UTC)
	m := &fakeMailer{}
	c := NewCodes(newFakeCodeStore(), m)
	f := fixture{codes: c, mailer: m, clock: &now}
	c.now = func() time.Time { return *f.clock }
	return f
}

func TestCodesRequestThenVerify(t *testing.T) {
	t.Parallel()
	f := newFixture()
	ctx := context.Background()
	ch, err := f.codes.Request(ctx, "a@b.kz", PurposeSignIn)
	if err != nil {
		t.Fatal(err)
	}
	if len(ch.Code) != CodeLength || len(f.mailer.sent) != 1 {
		t.Fatalf("want a %d-digit code sent once, got %q and %d emails", CodeLength, ch.Code, len(f.mailer.sent))
	}
	if err := f.codes.Verify(ctx, "a@b.kz", PurposeSignIn, ch.Code); err != nil {
		t.Fatalf("verify: %v", err)
	}
	if err := f.codes.Verify(ctx, "a@b.kz", PurposeSignIn, ch.Code); !errors.Is(err, ErrCodeExpired) {
		t.Fatalf("a code must work only once, got %v", err)
	}
}

func TestCodesRejectResendBeforeCooldown(t *testing.T) {
	t.Parallel()
	f := newFixture()
	ctx := context.Background()
	if _, err := f.codes.Request(ctx, "a@b.kz", PurposeSignIn); err != nil {
		t.Fatal(err)
	}
	if _, err := f.codes.Request(ctx, "a@b.kz", PurposeSignIn); !errors.Is(err, ErrResendTooSoon) {
		t.Fatalf("want ErrResendTooSoon, got %v", err)
	}
	*f.clock = f.clock.Add(ResendAfter)
	if _, err := f.codes.Request(ctx, "a@b.kz", PurposeSignIn); err != nil {
		t.Fatalf("resend after cooldown: %v", err)
	}
}

func TestCodesVerifyFailures(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name    string
		advance time.Duration
		guesses int
		want    error
	}{
		{name: "wrong code", guesses: 1, want: ErrInvalidCode},
		{name: "expired", advance: CodeTTL, guesses: 1, want: ErrCodeExpired},
		{name: "attempts exhausted", guesses: MaxAttempts + 1, want: ErrTooManyAttempts},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			f := newFixture()
			ctx := context.Background()
			ch, err := f.codes.Request(ctx, "a@b.kz", PurposeSignIn)
			if err != nil {
				t.Fatal(err)
			}
			*f.clock = f.clock.Add(tt.advance)
			wrong := "000000"
			if ch.Code == wrong {
				wrong = "111111"
			}
			for range tt.guesses {
				err = f.codes.Verify(ctx, "a@b.kz", PurposeSignIn, wrong)
			}
			if !errors.Is(err, tt.want) {
				t.Fatalf("got %v, want %v", err, tt.want)
			}
		})
	}
}
