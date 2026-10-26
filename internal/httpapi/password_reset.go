package httpapi

import (
	"errors"
	"net/http"
	"strings"

	"github.com/SWE-2026/biletflow-api/internal/auth"
	"github.com/SWE-2026/biletflow-api/internal/store"
	"github.com/labstack/echo/v4"
)

func (s *Server) requestPasswordReset(c echo.Context) error {
	email, err := decodeEmail(c)
	if err != nil {
		return err
	}
	ctx := c.Request().Context()
	if _, err := s.db.UserByEmail(ctx, email); errors.Is(err, store.ErrNotFound) {
		return c.JSON(http.StatusOK, s.challengeJSON(auth.Challenge{ExpiresIn: auth.CodeTTL, ResendIn: auth.ResendAfter}))
	} else if err != nil {
		return err
	}
	ch, err := s.codes.Request(ctx, email, auth.PurposePasswordReset)
	if err != nil {
		return err
	}
	return c.JSON(http.StatusOK, s.challengeJSON(ch))
}

type confirmPasswordResetRequest struct {
	Email    string `json:"email"`
	Code     string `json:"code"`
	Password string `json:"password"`
}

func (s *Server) confirmPasswordReset(c echo.Context) error {
	var in confirmPasswordResetRequest
	if err := decodeJSON(c, &in); err != nil {
		return err
	}
	email := normalizeEmail(in.Email)
	fe := fieldErrors{}
	fe.check(validEmail(email), "email", "Enter a valid email address.")
	fe.check(auth.CheckPasswordPolicy(in.Password) == nil, "password", "Use 8 to 72 characters.")
	if err := fe.err(); err != nil {
		return err
	}
	ctx := c.Request().Context()
	if err := s.codes.Verify(ctx, email, auth.PurposePasswordReset, strings.TrimSpace(in.Code)); err != nil {
		return err
	}
	hash, err := auth.HashPassword(in.Password)
	if err != nil {
		return err
	}
	u, err := s.db.ResetPassword(ctx, email, hash)
	if err != nil {
		return err
	}
	session, err := s.startSession(ctx, u)
	if err != nil {
		return err
	}
	return c.JSON(http.StatusOK, session)
}
