package httpapi

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/SWE-2026/biletflow-api/internal/auth"
	"github.com/SWE-2026/biletflow-api/internal/store"
	"github.com/labstack/echo/v4"
)

func TestToAPIErrorHidesInternals(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name     string
		give     error
		wantCode string
	}{
		{"unknown error", errors.New("pq: relation users does not exist"), "internal"},
		{"wrapped store error", errors.Join(errors.New("ctx"), store.ErrNotFound), "not_found"},
		{"auth error", auth.ErrInvalidCode, "invalid_code"},
		{"echo 404", echo.ErrNotFound, "not_found"},
		{"api error", errInvalidJSON, "invalid_json"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			if got := toAPIError(tt.give).Code; got != tt.wantCode {
				t.Fatalf("code = %q, want %q", got, tt.wantCode)
			}
		})
	}
}

func TestRequireAuthRejectsMissingOrMalformedToken(t *testing.T) {
	t.Parallel()
	s := &Server{}
	e := echo.New()
	e.HTTPErrorHandler = errorHandler(nil)
	e.GET("/private", func(c echo.Context) error { return c.NoContent(http.StatusNoContent) }, s.requireAuth)
	for _, header := range []string{"", "Bearer ", "Basic abc", "token-without-scheme"} {
		req := httptest.NewRequest(http.MethodGet, "/private", nil)
		req.Header.Set(echo.HeaderAuthorization, header)
		rec := httptest.NewRecorder()
		e.ServeHTTP(rec, req)
		if rec.Code != http.StatusUnauthorized {
			t.Fatalf("Authorization %q: got %d, want 401", header, rec.Code)
		}
	}
}
