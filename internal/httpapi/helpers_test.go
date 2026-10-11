package httpapi

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"
	"time"

	"github.com/SWE-2026/biletflow-api/internal/auth"
	"github.com/SWE-2026/biletflow-api/internal/mail"
	"github.com/SWE-2026/biletflow-api/internal/store"
	"github.com/labstack/echo/v4"
)

// newDBServer starts the API against the throwaway database in TEST_DB_DSN, or skips the test
// when it is unset so `go test ./...` stays database-free. Run DB-backed tests with `make test`.
func newDBServer(t *testing.T) *echo.Echo {
	t.Helper()
	dsn := os.Getenv("TEST_DB_DSN")
	if dsn == "" {
		t.Skip("TEST_DB_DSN not set; run `make test` for DB-backed tests")
	}
	ctx := context.Background()
	db, err := store.NewPostgres(ctx, dsn)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(db.Close)
	if err := db.Migrate(ctx); err != nil {
		t.Fatal(err)
	}
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	return New(db, auth.NewCodes(db, mail.LogMailer{Logger: logger}), Options{
		SessionTTL: time.Hour, ExposeCodes: true, Logger: logger, NoAuthRateLimit: true,
	})
}

// uniqueEmail keeps repeated runs against the same database from colliding.
func uniqueEmail(name string) string {
	b := make([]byte, 4)
	_, _ = rand.Read(b)
	return name + "-" + hex.EncodeToString(b) + "@test.biletflow.kz"
}

type client struct {
	t     *testing.T
	e     *echo.Echo
	token string
}

// do sends a JSON request and decodes the JSON response into out (when non-nil), failing the test
// unless the status is wantStatus.
func (c client) do(method, path string, body any, wantStatus int, out any) {
	c.t.Helper()
	var buf bytes.Buffer
	if body != nil {
		if err := json.NewEncoder(&buf).Encode(body); err != nil {
			c.t.Fatal(err)
		}
	}
	req := httptest.NewRequest(method, path, &buf)
	req.Header.Set(echo.HeaderContentType, echo.MIMEApplicationJSON)
	if c.token != "" {
		req.Header.Set(echo.HeaderAuthorization, "Bearer "+c.token)
	}
	rec := httptest.NewRecorder()
	c.e.ServeHTTP(rec, req)
	if rec.Code != wantStatus {
		c.t.Fatalf("%s %s: status %d, want %d; body %s", method, path, rec.Code, wantStatus, rec.Body.String())
	}
	if out != nil {
		if err := json.Unmarshal(rec.Body.Bytes(), out); err != nil {
			c.t.Fatalf("%s %s: decode: %v; body %s", method, path, err, rec.Body.String())
		}
	}
}

// signInWithCode runs the passwordless flow and returns a signed-in client.
func signInWithCode(t *testing.T, e *echo.Echo, email string) client {
	t.Helper()
	anon := client{t: t, e: e}
	var ch challengeResponse
	anon.do(http.MethodPost, "/api/v1/auth/otp/request", map[string]string{"email": email}, http.StatusOK, &ch)
	var s sessionResponse
	anon.do(http.MethodPost, "/api/v1/auth/otp/verify", map[string]string{"email": email, "code": ch.DemoCode}, http.StatusOK, &s)
	if !s.User.EmailVerified {
		t.Fatal("signing in with a code must verify the email")
	}
	return client{t: t, e: e, token: s.Token}
}
