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

// getPublicEvent serves the attendee event page: a published public or unlisted event and its
// visible ticket types. Drafts, cancelled and private events answer 404.
func (s *Server) getPublicEvent(c echo.Context) error {
	id, err := eventID(c)
	if err != nil {
		return err
	}
	ctx := c.Request().Context()
	event, err := s.db.PublicEvent(ctx, id)
	if err != nil {
		return err
	}
	types, err := s.db.ListVisibleTicketTypes(ctx, id)
	if err != nil {
		return err
	}
	if types == nil {
		types = []store.TicketType{}
	}
	return c.JSON(http.StatusOK, map[string]any{"event": event, "ticketTypes": types})
}
