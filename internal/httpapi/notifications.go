package httpapi

import (
	"net/http"

	"github.com/SWE-2026/biletflow-api/internal/store"
	"github.com/labstack/echo/v4"
)

func (s *Server) listNotifications(c echo.Context) error {
	list, unread, err := s.db.ListNotifications(c.Request().Context(), currentUser(c).ID, c.QueryParam("unread") == "true")
	if err != nil {
		return err
	}
	if list == nil {
		list = []store.Notification{}
	}
	return c.JSON(http.StatusOK, map[string]any{"notifications": list, "unread": unread})
}

func (s *Server) readNotification(c echo.Context) error {
	id, err := pathID(c, "id")
	if err != nil {
		return err
	}
	if err := s.db.MarkNotificationsRead(c.Request().Context(), currentUser(c).ID, id); err != nil {
		return err
	}
	return c.NoContent(http.StatusNoContent)
}

func (s *Server) readAllNotifications(c echo.Context) error {
	if err := s.db.MarkNotificationsRead(c.Request().Context(), currentUser(c).ID, ""); err != nil {
		return err
	}
	return c.NoContent(http.StatusNoContent)
}
