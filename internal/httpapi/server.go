// Package httpapi is the REST API consumed by the web app and the Event Admin mobile app.
// The contract is documented in docs/api.md.
package httpapi

import (
	"log/slog"
	"net/http"
	"time"

	"github.com/SWE-2026/biletflow-api/internal/auth"
	"github.com/SWE-2026/biletflow-api/internal/store"
	"github.com/labstack/echo/v4"
	"github.com/labstack/echo/v4/middleware"
	"golang.org/x/time/rate"
)

type Options struct {
	SessionTTL time.Duration
	// ExposeCodes returns one-time codes in responses (as demoCode). Local development only.
	ExposeCodes bool
	CORSOrigins []string
	Logger      *slog.Logger
	// NoAuthRateLimit turns off per-IP throttling of /auth endpoints. Tests only.
	NoAuthRateLimit bool
}

type Server struct {
	db    *store.Postgres
	codes *auth.Codes
	opts  Options
}

func New(db *store.Postgres, codes *auth.Codes, opts Options) *echo.Echo {
	s := &Server{db: db, codes: codes, opts: opts}
	e := echo.New()
	e.HideBanner = true
	e.HTTPErrorHandler = errorHandler(opts.Logger)
	e.Use(middleware.Recover())
	e.Use(middleware.BodyLimit("1M"))
	e.Use(middleware.CORSWithConfig(middleware.CORSConfig{
		AllowOrigins: opts.CORSOrigins,
		AllowHeaders: []string{echo.HeaderAuthorization, echo.HeaderContentType, echo.HeaderAccept},
		MaxAge:       600,
	}))
	e.GET("/health", s.health)
	s.routes(e.Group("/api/v1"))
	return e
}

func (s *Server) routes(v1 *echo.Group) {
	authGroup := v1.Group("/auth")
	if !s.opts.NoAuthRateLimit {
		authGroup.Use(authRateLimit())
	}
	authGroup.POST("/otp/request", s.requestSignInCode)
	authGroup.POST("/otp/verify", s.verifySignInCode)
	authGroup.POST("/signup", s.signup)
	authGroup.POST("/login", s.login)
	authGroup.POST("/password/reset/request", s.requestPasswordReset)
	authGroup.POST("/password/reset/confirm", s.confirmPasswordReset)
	authGroup.POST("/logout", s.logout, s.requireAuth)

	v1.GET("/me", s.me, s.requireAuth)
	v1.GET("/events", s.listEvents)

	organizer := v1.Group("/organizer", s.requireAuth)
	organizer.POST("/profile", s.createOrganizer)
	organizer.GET("/profile", s.getOrganizer, s.requireOrganizer)
	organizer.PUT("/profile", s.updateOrganizer, s.requireOrganizer)
	events := organizer.Group("/events", s.requireOrganizer)
	events.POST("", s.createEvent)
	events.GET("", s.listOrganizerEvents)
	events.GET("/:id", s.getOrganizerEvent)
	events.PUT("/:id", s.updateEvent)
	events.POST("/:id/publish", s.publishEvent)
	events.POST("/:id/unpublish", s.unpublishEvent)
	events.POST("/:id/cancel", s.cancelEvent)
	events.GET("/:id/ticket-types", s.listTicketTypes)
	events.POST("/:id/ticket-types", s.createTicketType)
	events.PUT("/:id/ticket-types/:typeId", s.updateTicketType)
	events.DELETE("/:id/ticket-types/:typeId", s.deleteTicketType)
}

// authRateLimit slows down code and password guessing: about one request per second per IP, with bursts of 10.
func authRateLimit() echo.MiddlewareFunc {
	return middleware.RateLimiter(middleware.NewRateLimiterMemoryStoreWithConfig(middleware.RateLimiterMemoryStoreConfig{
		Rate: rate.Limit(1), Burst: 10, ExpiresIn: 3 * time.Minute,
	}))
}

func (s *Server) health(c echo.Context) error {
	if err := s.db.Ping(c.Request().Context()); err != nil {
		return c.JSON(http.StatusServiceUnavailable, map[string]string{"status": "unavailable"})
	}
	return c.JSON(http.StatusOK, map[string]string{"status": "ok"})
}
