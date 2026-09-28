package app

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestTokenHashIsStableAndNotToken(t *testing.T) {
	token := "a-session-token"
	if tokenHash(token) == token {
		t.Fatal("token must not be stored directly")
	}
	if tokenHash(token) != tokenHash(token) {
		t.Fatal("hash must be stable")
	}
}

func TestMissingBearerTokenIsRejected(t *testing.T) {
	api := &API{}
	e := echoForTest(api)
	req := httptest.NewRequest(http.MethodGet, "/private", nil)
	rec := httptest.NewRecorder()
	e.ServeHTTP(rec, req)
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("got %d, want %d", rec.Code, http.StatusUnauthorized)
	}
}
