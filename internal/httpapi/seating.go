package httpapi

import (
	"net/http"
	"strings"

	"github.com/SWE-2026/biletflow-api/internal/store"
	"github.com/labstack/echo/v4"
)

const maxSeatsPerOrder = 10

func (s *Server) getPublicSeating(c echo.Context) error {
	id, err := eventID(c)
	if err != nil {
		return err
	}
	seating, err := s.db.PublicSeating(c.Request().Context(), id)
	if err != nil {
		return err
	}
	return c.JSON(http.StatusOK, map[string]any{"seating": seating})
}

func (s *Server) getOrganizerSeating(c echo.Context) error {
	id, err := eventID(c)
	if err != nil {
		return err
	}
	seating, err := s.db.OrganizerEventSeating(c.Request().Context(), organizerID(c), id)
	if err != nil {
		return err
	}
	return c.JSON(http.StatusOK, map[string]any{"seating": seating, "layouts": store.SeatingLayouts})
}

func decodeSeating(c echo.Context) (store.SeatingInput, error) {
	var in store.SeatingInput
	if err := decodeJSON(c, &in); err != nil {
		return in, err
	}
	for category, id := range in.Categories {
		in.Categories[category] = strings.ToLower(id)
	}
	fe := fieldErrors{}
	fe.check(in.Mode == "general" || in.Mode == "assigned", "mode", "Choose general or assigned.")
	if in.Mode == "assigned" {
		layout, ok := store.SeatingLayoutByKey(in.LayoutKey)
		fe.check(ok, "layoutKey", "Choose one of the predefined layouts.")
		for _, category := range layout.Categories {
			fe.check(eventIDPattern.MatchString(in.Categories[category]), "categories", "Choose a ticket type for every seat category.")
		}
	}
	return in, fe.err()
}

func (s *Server) configureSeating(c echo.Context) error {
	id, err := eventID(c)
	if err != nil {
		return err
	}
	in, err := decodeSeating(c)
	if err != nil {
		return err
	}
	seating, err := s.db.ConfigureSeating(c.Request().Context(), organizerID(c), currentUser(c).ID, id, in)
	if err != nil {
		return err
	}
	return c.JSON(http.StatusOK, map[string]any{"seating": seating, "layouts": store.SeatingLayouts})
}
