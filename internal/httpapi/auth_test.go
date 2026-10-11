package httpapi

import (
	"net/http"
	"testing"
)

func TestSignInWithCodeAndPassword(t *testing.T) {
	e := newDBServer(t)

	// Email code: the first sign-in creates a verified account.
	user := signInWithCode(t, e, uniqueEmail("code"))
	var me struct{ User userResponse }
	user.do(http.MethodGet, "/api/v1/me", nil, http.StatusOK, &me)
	if me.User.HasPassword {
		t.Fatal("a code-only account must not have a password")
	}
	user.do(http.MethodPost, "/api/v1/auth/logout", nil, http.StatusNoContent, nil)
	user.do(http.MethodGet, "/api/v1/me", nil, http.StatusUnauthorized, nil)

	// Password: signup, duplicate, wrong and right password.
	anon := client{t: t, e: e}
	email := uniqueEmail("password")
	creds := map[string]string{"email": email, "password": "correct horse", "displayName": "Aru"}
	anon.do(http.MethodPost, "/api/v1/auth/signup", creds, http.StatusCreated, nil)
	anon.do(http.MethodPost, "/api/v1/auth/signup", creds, http.StatusConflict, nil)
	anon.do(http.MethodPost, "/api/v1/auth/login", map[string]string{"email": email, "password": "wrong password"}, http.StatusUnauthorized, nil)
	var s sessionResponse
	anon.do(http.MethodPost, "/api/v1/auth/login", map[string]string{"email": email, "password": "correct horse"}, http.StatusOK, &s)
	if !s.User.HasPassword || s.User.EmailVerified {
		t.Fatalf("password signup: want hasPassword and unverified email, got %+v", s.User)
	}

	// The same email can also sign in by code, which verifies it.
	signInWithCode(t, e, email)
}
