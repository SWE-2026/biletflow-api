package httpapi

import (
	"context"
	"errors"
	"net/http"
	"strings"
	"time"

	"github.com/SWE-2026/biletflow-api/internal/auth"
	"github.com/SWE-2026/biletflow-api/internal/store"
	"github.com/labstack/echo/v4"
)

var (
	errInvalidCredentials = newError(http.StatusUnauthorized, "invalid_credentials", "Email or password is incorrect.")
	errEmailTaken         = newError(http.StatusConflict, "email_taken", "An account with this email already exists. Sign in instead.")
)

type userResponse struct {
	ID            string    `json:"id"`
	Email         string    `json:"email"`
	DisplayName   string    `json:"displayName"`
	Role          string    `json:"role"`
	OrganizerID   *string   `json:"organizerId"`
	EmailVerified bool      `json:"emailVerified"`
	HasPassword   bool      `json:"hasPassword"`
	CreatedAt     time.Time `json:"createdAt"`
}

func userJSON(u store.User) userResponse {
	return userResponse{
		ID: u.ID, Email: u.Email, DisplayName: u.DisplayName, Role: u.Role, OrganizerID: u.OrganizerID,
		EmailVerified: u.EmailVerifiedAt != nil, HasPassword: u.PasswordHash != nil, CreatedAt: u.CreatedAt,
	}
}

type sessionResponse struct {
	Token string       `json:"token"`
	User  userResponse `json:"user"`
}

type challengeResponse struct {
	ExpiresInSeconds int `json:"expiresInSeconds"`
	ResendInSeconds  int `json:"resendInSeconds"`
	// DemoCode is the code itself, only in local development (APP_ENV=local).
	DemoCode string `json:"demoCode,omitempty"`
}

func (s *Server) challengeJSON(ch auth.Challenge) challengeResponse {
	r := challengeResponse{ExpiresInSeconds: int(ch.ExpiresIn.Seconds()), ResendInSeconds: int(ch.ResendIn.Seconds())}
	if s.opts.ExposeCodes {
		r.DemoCode = ch.Code
	}
	return r
}

func (s *Server) startSession(ctx context.Context, u store.User) (sessionResponse, error) {
	token, hash := auth.NewSessionToken()
	if err := s.db.CreateSession(ctx, u.ID, hash, time.Now().Add(s.opts.SessionTTL)); err != nil {
		return sessionResponse{}, err
	}
	return sessionResponse{Token: token, User: userJSON(u)}, nil
}

type emailRequest struct {
	Email string `json:"email"`
}

func decodeEmail(c echo.Context) (string, error) {
	var in emailRequest
	if err := decodeJSON(c, &in); err != nil {
		return "", err
	}
	email := normalizeEmail(in.Email)
	fe := fieldErrors{}
	fe.check(validEmail(email), "email", "Enter a valid email address.")
	return email, fe.err()
}

// requestSignInCode emails a one-time sign-in code. The first sign-in creates the account.
func (s *Server) requestSignInCode(c echo.Context) error {
	email, err := decodeEmail(c)
	if err != nil {
		return err
	}
	ch, err := s.codes.Request(c.Request().Context(), email, auth.PurposeSignIn)
	if err != nil {
		return err
	}
	return c.JSON(http.StatusOK, s.challengeJSON(ch))
}

type verifyCodeRequest struct {
	Email string `json:"email"`
	Code  string `json:"code"`
}

func (s *Server) verifySignInCode(c echo.Context) error {
	var in verifyCodeRequest
	if err := decodeJSON(c, &in); err != nil {
		return err
	}
	email := normalizeEmail(in.Email)
	ctx := c.Request().Context()
	if err := s.codes.Verify(ctx, email, auth.PurposeSignIn, strings.TrimSpace(in.Code)); err != nil {
		return err
	}
	u, err := s.db.UpsertVerifiedUser(ctx, email)
	if err != nil {
		return err
	}
	session, err := s.startSession(ctx, u)
	if err != nil {
		return err
	}
	return c.JSON(http.StatusOK, session)
}

type signupRequest struct {
	Email       string `json:"email"`
	Password    string `json:"password"`
	DisplayName string `json:"displayName"`
}

// signup creates a password account and signs it in.
func (s *Server) signup(c echo.Context) error {
	var in signupRequest
	if err := decodeJSON(c, &in); err != nil {
		return err
	}
	email, name := normalizeEmail(in.Email), strings.TrimSpace(in.DisplayName)
	fe := fieldErrors{}
	fe.check(validEmail(email), "email", "Enter a valid email address.")
	fe.check(auth.CheckPasswordPolicy(in.Password) == nil, "password", "Use 8 to 72 characters.")
	fe.check(name != "" && len(name) <= 100, "displayName", "Enter your name (up to 100 characters).")
	if err := fe.err(); err != nil {
		return err
	}
	hash, err := auth.HashPassword(in.Password)
	if err != nil {
		return err
	}
	ctx := c.Request().Context()
	u, err := s.db.CreateUser(ctx, email, name, hash)
	if store.IsUniqueViolation(err) {
		return errEmailTaken
	}
	if err != nil {
		return err
	}
	session, err := s.startSession(ctx, u)
	if err != nil {
		return err
	}
	return c.JSON(http.StatusCreated, session)
}

type loginRequest struct {
	Email    string `json:"email"`
	Password string `json:"password"`
}

func (s *Server) login(c echo.Context) error {
	var in loginRequest
	if err := decodeJSON(c, &in); err != nil {
		return err
	}
	ctx := c.Request().Context()
	u, err := s.db.UserByEmail(ctx, normalizeEmail(in.Email))
	if err != nil && !errors.Is(err, store.ErrNotFound) {
		return err
	}
	if !auth.PasswordMatches(u.PasswordHash, in.Password) {
		return errInvalidCredentials
	}
	session, err := s.startSession(ctx, u)
	if err != nil {
		return err
	}
	return c.JSON(http.StatusOK, session)
}

func (s *Server) logout(c echo.Context) error {
	if err := s.db.RevokeSession(c.Request().Context(), c.Get(ctxTokenHash).(string)); err != nil {
		return err
	}
	return c.NoContent(http.StatusNoContent)
}
