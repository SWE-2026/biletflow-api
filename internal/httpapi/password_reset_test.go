package httpapi

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestPasswordResetValidatesBeforeTouchingCodes(t *testing.T) {
	t.Parallel()
	e := New(nil, nil, Options{NoAuthRateLimit: true})
	tests := []struct {
		name      string
		path      string
		give      string
		wantField string
	}{
		{"request bad email", "/api/v1/auth/password/reset/request", `{"email":"nope"}`, "email"},
		{"confirm short password", "/api/v1/auth/password/reset/confirm", `{"email":"a@b.kz","code":"123456","password":"short"}`, "password"},
		{"confirm bad email", "/api/v1/auth/password/reset/confirm", `{"email":"nope","code":"123456","password":"long enough"}`, "email"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			req := httptest.NewRequest(http.MethodPost, tt.path, strings.NewReader(tt.give))
			req.Header.Set("Content-Type", "application/json")
			rec := httptest.NewRecorder()
			e.ServeHTTP(rec, req)
			var body Error
			if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
				t.Fatal(err)
			}
			if rec.Code != http.StatusBadRequest || body.Code != "validation_failed" || body.Fields[tt.wantField] == "" {
				t.Fatalf("got %d %+v, want 400 validation_failed on %s", rec.Code, body, tt.wantField)
			}
		})
	}
}

func TestPasswordReset(t *testing.T) {
	e := newDBServer(t)
	anon := client{t: t, e: e}
	email := uniqueEmail("reset")
	var old sessionResponse
	anon.do(http.MethodPost, "/api/v1/auth/signup", map[string]string{"email": email, "password": "old password", "displayName": "Aru"}, http.StatusCreated, &old)
	oldUser := client{t: t, e: e, token: old.Token}

	var unknown challengeResponse
	anon.do(http.MethodPost, "/api/v1/auth/password/reset/request", map[string]string{"email": uniqueEmail("nobody")}, http.StatusOK, &unknown)
	if unknown.DemoCode != "" || unknown.ExpiresInSeconds == 0 {
		t.Fatalf("unknown email: want a challenge without a code, got %+v", unknown)
	}

	var ch challengeResponse
	anon.do(http.MethodPost, "/api/v1/auth/password/reset/request", map[string]string{"email": email}, http.StatusOK, &ch)
	anon.do(http.MethodPost, "/api/v1/auth/otp/verify", map[string]string{"email": email, "code": ch.DemoCode}, http.StatusGone, nil)

	wrong := "000000"
	if ch.DemoCode == wrong {
		wrong = "111111"
	}
	confirm := func(code string, status int, out any) {
		anon.do(http.MethodPost, "/api/v1/auth/password/reset/confirm",
			map[string]string{"email": email, "code": code, "password": "new password"}, status, out)
	}
	confirm(wrong, http.StatusBadRequest, nil)

	var s sessionResponse
	confirm(ch.DemoCode, http.StatusOK, &s)
	if !s.User.HasPassword || !s.User.EmailVerified {
		t.Fatalf("after reset: want password and verified email, got %+v", s.User)
	}
	confirm(ch.DemoCode, http.StatusGone, nil)

	oldUser.do(http.MethodGet, "/api/v1/me", nil, http.StatusUnauthorized, nil)
	client{t: t, e: e, token: s.Token}.do(http.MethodGet, "/api/v1/me", nil, http.StatusOK, nil)
	anon.do(http.MethodPost, "/api/v1/auth/login", map[string]string{"email": email, "password": "old password"}, http.StatusUnauthorized, nil)
	anon.do(http.MethodPost, "/api/v1/auth/login", map[string]string{"email": email, "password": "new password"}, http.StatusOK, nil)
}

func TestPasswordResetSetsFirstPasswordForCodeAccount(t *testing.T) {
	e := newDBServer(t)
	email := uniqueEmail("codeonly")
	signInWithCode(t, e, email)
	anon := client{t: t, e: e}
	var ch challengeResponse
	anon.do(http.MethodPost, "/api/v1/auth/password/reset/request", map[string]string{"email": email}, http.StatusOK, &ch)
	var s sessionResponse
	anon.do(http.MethodPost, "/api/v1/auth/password/reset/confirm",
		map[string]string{"email": email, "code": ch.DemoCode, "password": "first password"}, http.StatusOK, &s)
	if !s.User.HasPassword {
		t.Fatal("a code-only account must have a password after reset")
	}
	anon.do(http.MethodPost, "/api/v1/auth/login", map[string]string{"email": email, "password": "first password"}, http.StatusOK, nil)
}
