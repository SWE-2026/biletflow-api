package httpapi

import (
	"net/http"

	"github.com/SWE-2026/biletflow-api/internal/store"
	"github.com/labstack/echo/v4"
)

func (s *Server) me(c echo.Context) error {
	return c.JSON(http.StatusOK, map[string]any{"user": userJSON(currentUser(c))})
}

func (s *Server) listEvents(c echo.Context) error {
	events, err := s.db.ListPublishedEvents(c.Request().Context())
	if err != nil {
		return err
	}
	if events == nil {
		events = []store.Event{}
	}
	return c.JSON(http.StatusOK, map[string]any{"events": events})
}
