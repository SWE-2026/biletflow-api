package httpapi

import (
	"net/http"

	"github.com/SWE-2026/biletflow-api/internal/store"
	"github.com/labstack/echo/v4"
)

var errRegisterUnverified = newError(http.StatusForbidden, "email_unverified", "Verify your email before registering for an event.")

type registrationInput struct {
	TicketTypeID string `json:"ticketTypeId"`
	Quantity     int    `json:"quantity"`
}

func validateRegistration(in registrationInput) error {
	fe := fieldErrors{}
	fe.check(eventIDPattern.MatchString(in.TicketTypeID), "ticketTypeId", "Choose a ticket type.")
	fe.check(in.Quantity >= 1 && in.Quantity <= 100, "quantity", "Choose a quantity from 1 to 100.")
	return fe.err()
}

// register issues free tickets to the signed-in attendee. Paid ticket types need checkout.
func (s *Server) register(c echo.Context) error {
	id, err := eventID(c)
	if err != nil {
		return err
	}
	var in registrationInput
	if err := decodeJSON(c, &in); err != nil {
		return err
	}
	if err := validateRegistration(in); err != nil {
		return err
	}
	u := currentUser(c)
	if u.EmailVerifiedAt == nil {
		return errRegisterUnverified
	}
	tickets, err := s.db.RegisterFree(c.Request().Context(), u.ID, id, in.TicketTypeID, in.Quantity)
	if err != nil {
		return err
	}
	return c.JSON(http.StatusCreated, map[string]any{"orderId": tickets[0].OrderID, "tickets": tickets})
}

func (s *Server) listMyTickets(c echo.Context) error {
	tickets, err := s.db.ListUserTickets(c.Request().Context(), currentUser(c).ID)
	if err != nil {
		return err
	}
	if tickets == nil {
		tickets = []store.Ticket{}
	}
	return c.JSON(http.StatusOK, map[string]any{"tickets": tickets})
}
