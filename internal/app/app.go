package app

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"net/http"
	"strings"
	"time"

	"github.com/example/swe-api/internal/store"
	"github.com/labstack/echo/v4"
	"golang.org/x/crypto/bcrypt"
)

type API struct {
	db         *store.Postgres
	sessionTTL time.Duration
}

func New(db *store.Postgres, sessionTTL time.Duration) *echo.Echo {
	a := &API{db: db, sessionTTL: sessionTTL}
	e := echo.New()
	e.HideBanner = true
	e.GET("/health", a.health)
	v1 := e.Group("/api/v1")
	v1.POST("/auth/signup", a.signup)
	v1.POST("/auth/login", a.login)
	v1.POST("/auth/logout", a.logout, a.requireAuth)
	v1.GET("/me", a.me, a.requireAuth)
	v1.GET("/events", a.listEvents)
	return e
}

type credentials struct {
	Email       string `json:"email"`
	Password    string `json:"password"`
	DisplayName string `json:"displayName"`
}
type userResponse struct {
	ID          string    `json:"id"`
	Email       string    `json:"email"`
	DisplayName string    `json:"displayName"`
	Role        string    `json:"role"`
	CreatedAt   time.Time `json:"createdAt"`
}

func userJSON(u store.User) userResponse {
	return userResponse{u.ID, u.Email, u.DisplayName, u.Role, u.CreatedAt}
}
func (a *API) health(c echo.Context) error {
	if err := a.db.Ping(c.Request().Context()); err != nil {
		return c.JSON(503, map[string]string{"status": "unavailable"})
	}
	return c.JSON(200, map[string]string{"status": "ok"})
}
func (a *API) signup(c echo.Context) error {
	var in credentials
	if err := c.Bind(&in); err != nil {
		return badRequest(c, "invalid JSON")
	}
	in.Email = strings.TrimSpace(in.Email)
	in.DisplayName = strings.TrimSpace(in.DisplayName)
	if !strings.Contains(in.Email, "@") || len(in.Password) < 12 || in.DisplayName == "" {
		return badRequest(c, "email, displayName, and a password of at least 12 characters are required")
	}
	hash, err := bcrypt.GenerateFromPassword([]byte(in.Password), bcrypt.DefaultCost)
	if err != nil {
		return err
	}
	u, err := a.db.CreateUser(c.Request().Context(), in.Email, in.DisplayName, string(hash))
	if err != nil {
		if store.IsUniqueViolation(err) {
			return c.JSON(409, map[string]string{"error": "email already registered"})
		}
		return err
	}
	token, err := a.issueSession(c.Request().Context(), u.ID)
	if err != nil {
		return err
	}
	return c.JSON(201, map[string]any{"user": userJSON(u), "token": token})
}
func (a *API) login(c echo.Context) error {
	var in credentials
	if err := c.Bind(&in); err != nil {
		return badRequest(c, "invalid JSON")
	}
	u, err := a.db.UserByEmail(c.Request().Context(), strings.TrimSpace(in.Email))
	if err != nil || bcrypt.CompareHashAndPassword([]byte(u.PasswordHash), []byte(in.Password)) != nil {
		return c.JSON(401, map[string]string{"error": "invalid email or password"})
	}
	token, err := a.issueSession(c.Request().Context(), u.ID)
	if err != nil {
		return err
	}
	return c.JSON(200, map[string]any{"user": userJSON(u), "token": token})
}
func (a *API) issueSession(ctx context.Context, userID string) (string, error) {
	raw := make([]byte, 32)
	if _, err := rand.Read(raw); err != nil {
		return "", err
	}
	token := base64.RawURLEncoding.EncodeToString(raw)
	return token, a.db.CreateSession(ctx, userID, tokenHash(token), time.Now().Add(a.sessionTTL))
}
func tokenHash(token string) string {
	sum := sha256.Sum256([]byte(token))
	return base64.RawURLEncoding.EncodeToString(sum[:])
}
func (a *API) requireAuth(next echo.HandlerFunc) echo.HandlerFunc {
	return func(c echo.Context) error {
		token := strings.TrimPrefix(c.Request().Header.Get("Authorization"), "Bearer ")
		if token == "" || token == c.Request().Header.Get("Authorization") {
			return c.JSON(401, map[string]string{"error": "missing bearer token"})
		}
		u, err := a.db.UserByTokenHash(c.Request().Context(), tokenHash(token))
		if err != nil {
			return c.JSON(401, map[string]string{"error": "invalid or expired token"})
		}
		c.Set("user", u)
		c.Set("tokenHash", tokenHash(token))
		return next(c)
	}
}
func (a *API) me(c echo.Context) error {
	return c.JSON(200, map[string]any{"user": userJSON(c.Get("user").(store.User))})
}
func (a *API) logout(c echo.Context) error {
	if err := a.db.RevokeSession(c.Request().Context(), c.Get("tokenHash").(string)); err != nil {
		return err
	}
	return c.NoContent(204)
}
func (a *API) listEvents(c echo.Context) error {
	events, err := a.db.ListPublishedEvents(c.Request().Context())
	if err != nil {
		return err
	}
	return c.JSON(200, map[string]any{"events": events})
}
func badRequest(c echo.Context, message string) error {
	return c.JSON(http.StatusBadRequest, map[string]string{"error": message})
}
