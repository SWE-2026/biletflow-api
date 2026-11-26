package httpapi

import (
	"log/slog"
	"strings"

	"github.com/SWE-2026/biletflow-api/internal/auth"
	"github.com/SWE-2026/biletflow-api/internal/store"
	"github.com/labstack/echo/v4"
	"github.com/labstack/echo/v4/middleware"
)

// requestLogger logs each request's method, path, status and duration. It never logs the query
// string, headers or body, which can carry tokens, one-time codes, emails or QR tokens.
func requestLogger(logger *slog.Logger) echo.MiddlewareFunc {
	return middleware.RequestLoggerWithConfig(middleware.RequestLoggerConfig{
		HandleError: true, // run the error handler first so the logged status is the real one
		LogMethod:   true,
		LogURIPath:  true,
		LogStatus:   true,
		LogLatency:  true,
		LogValuesFunc: func(c echo.Context, v middleware.RequestLoggerValues) error {
			logger.InfoContext(c.Request().Context(), "request",
				"method", v.Method, "path", v.URIPath, "status", v.Status, "ms", v.Latency.Milliseconds())
			return nil
		},
	})
}

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

func (s *Server) requireOrganizer(next echo.HandlerFunc) echo.HandlerFunc {
	return func(c echo.Context) error {
		u := currentUser(c)
		if u.Role != "organizer" || u.OrganizerID == nil {
			return errForbidden
		}
		return next(c)
	}
}
