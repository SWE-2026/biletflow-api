package httpapi

import (
	"net/http"
	"strings"

	"github.com/SWE-2026/biletflow-api/internal/store"
	"github.com/labstack/echo/v4"
)

// maxPriceAmount caps a ticket price at 10,000,000 KZT, well above any real event.
const maxPriceAmount = 10_000_000

func decodeTicketType(c echo.Context) (store.TicketTypeInput, error) {
	var in store.TicketTypeInput
	if err := decodeJSON(c, &in); err != nil {
		return in, err
	}
	in.Name = strings.TrimSpace(in.Name)
	in.Description = strings.TrimSpace(in.Description)
	return in, validateTicketType(in)
}

func validateTicketType(in store.TicketTypeInput) error {
	fe := fieldErrors{}
	fe.check(len(in.Name) > 0 && len(in.Name) <= 100, "name", "Enter a name (up to 100 characters).")
	fe.check(len(in.Description) <= 2000, "description", "Use up to 2000 characters.")
	fe.check(in.PriceAmount >= 0 && in.PriceAmount <= maxPriceAmount, "priceAmount", "Choose a price from 0 to 10000000 KZT.")
	fe.check(in.Quantity > 0 && in.Quantity <= 1000000, "quantity", "Choose a quantity from 1 to 1000000.")
	fe.check(in.SalesStartsAt == nil || in.SalesEndsAt == nil || in.SalesEndsAt.After(*in.SalesStartsAt), "salesEndsAt", "Sales end must follow sales start.")
	fe.check(in.MaxPerOrder >= 1 && in.MaxPerOrder <= 100, "maxPerOrder", "Choose a per-order limit from 1 to 100.")
	fe.check(in.SortOrder >= 0 && in.SortOrder <= 10000, "sortOrder", "Choose a sort order from 0 to 10000.")
	return fe.err()
}

// ticketTypePath returns the validated event and ticket type IDs from the path.
func ticketTypePath(c echo.Context) (string, string, error) {
	id, err := eventID(c)
	if err != nil {
		return "", "", err
	}
	typeID := c.Param("typeId")
	if !eventIDPattern.MatchString(typeID) {
		return "", "", errNotFound
	}
	return id, typeID, nil
}

func (s *Server) listTicketTypes(c echo.Context) error {
	id, err := eventID(c)
	if err != nil {
		return err
	}
	types, err := s.db.ListTicketTypes(c.Request().Context(), organizerID(c), id)
	if err != nil {
		return err
	}
	if types == nil {
		types = []store.TicketType{}
	}
	return c.JSON(http.StatusOK, map[string]any{"ticketTypes": types})
}

func (s *Server) createTicketType(c echo.Context) error {
	id, err := eventID(c)
	if err != nil {
		return err
	}
	in, err := decodeTicketType(c)
	if err != nil {
		return err
	}
	t, err := s.db.CreateTicketType(c.Request().Context(), organizerID(c), id, in)
	if err != nil {
		return err
	}
	return c.JSON(http.StatusCreated, map[string]any{"ticketType": t})
}

func (s *Server) updateTicketType(c echo.Context) error {
	id, typeID, err := ticketTypePath(c)
	if err != nil {
		return err
	}
	in, err := decodeTicketType(c)
	if err != nil {
		return err
	}
	t, err := s.db.UpdateTicketType(c.Request().Context(), organizerID(c), id, typeID, in)
	if err != nil {
		return err
	}
	return c.JSON(http.StatusOK, map[string]any{"ticketType": t})
}

func (s *Server) deleteTicketType(c echo.Context) error {
	id, typeID, err := ticketTypePath(c)
	if err != nil {
		return err
	}
	if err := s.db.DeleteTicketType(c.Request().Context(), organizerID(c), id, typeID); err != nil {
		return err
	}
	return c.NoContent(http.StatusNoContent)
}
