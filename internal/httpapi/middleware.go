package httpapi

import (
	"strings"

	"github.com/SWE-2026/biletflow-api/internal/auth"
	"github.com/SWE-2026/biletflow-api/internal/store"
	"github.com/labstack/echo/v4"
)

const (
	ctxUser      = "user"
	ctxTokenHash = "tokenHash"
)

func (s *Server) requireAuth(next echo.HandlerFunc) echo.HandlerFunc {
	return func(c echo.Context) error {
		token, ok := strings.CutPrefix(c.Request().Header.Get(echo.HeaderAuthorization), "Bearer ")
		if !ok || token == "" {
			return errUnauthorized
		}
		hash := auth.TokenHash(token)
		u, err := s.db.UserByTokenHash(c.Request().Context(), hash)
		if err != nil {
			return errUnauthorized
		}
		c.Set(ctxUser, u)
		c.Set(ctxTokenHash, hash)
		return next(c)
	}
}

func currentUser(c echo.Context) store.User { return c.Get(ctxUser).(store.User) }
