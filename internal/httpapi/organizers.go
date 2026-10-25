package httpapi

import (
	"net/http"
	"strings"

	"github.com/SWE-2026/biletflow-api/internal/store"
	"github.com/labstack/echo/v4"
)

func organizerID(c echo.Context) string { return *currentUser(c).OrganizerID }

func decodeOrganizer(c echo.Context) (store.OrganizerInput, error) {
	var in store.OrganizerInput
	if err := decodeJSON(c, &in); err != nil {
		return in, err
	}
	in.Type = strings.TrimSpace(in.Type)
	in.Name = strings.TrimSpace(in.Name)
	in.ContactEmail = normalizeEmail(in.ContactEmail)
	in.ContactPhone = strings.TrimSpace(in.ContactPhone)
	in.PayoutAccountReference = strings.TrimSpace(in.PayoutAccountReference)
	fe := fieldErrors{}
	fe.check(in.Type == "individual" || in.Type == "business", "type", "Choose individual or business.")
	fe.check(len(in.Name) > 0 && len(in.Name) <= 200, "name", "Enter an organizer name (up to 200 characters).")
	fe.check(validEmail(in.ContactEmail), "contactEmail", "Enter a valid contact email address.")
	fe.check(len(in.ContactPhone) <= 32, "contactPhone", "Use up to 32 characters.")
	fe.check(len(in.PayoutAccountReference) <= 100, "payoutAccountReference", "Use up to 100 characters.")
	return in, fe.err()
}

func (s *Server) createOrganizer(c echo.Context) error {
	in, err := decodeOrganizer(c)
	if err != nil {
		return err
	}
	o, u, err := s.db.CreateOrganizer(c.Request().Context(), currentUser(c).ID, in)
	if err != nil {
		return err
	}
	return c.JSON(http.StatusCreated, map[string]any{"organizer": o, "user": userJSON(u)})
}

func (s *Server) getOrganizer(c echo.Context) error {
	o, err := s.db.OrganizerByID(c.Request().Context(), organizerID(c))
	if err != nil {
		return err
	}
	return c.JSON(http.StatusOK, map[string]any{"organizer": o})
}

func (s *Server) updateOrganizer(c echo.Context) error {
	in, err := decodeOrganizer(c)
	if err != nil {
		return err
	}
	o, err := s.db.UpdateOrganizer(c.Request().Context(), organizerID(c), in)
	if err != nil {
		return err
	}
	return c.JSON(http.StatusOK, map[string]any{"organizer": o})
}
