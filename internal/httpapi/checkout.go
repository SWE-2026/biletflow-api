package httpapi

import (
	"net/http"
	"strings"

	"github.com/SWE-2026/biletflow-api/internal/store"
	"github.com/labstack/echo/v4"
)

const maxCheckoutItems = 10

func decodeCheckout(c echo.Context, accountEmail string) (store.CheckoutInput, error) {
	var in store.CheckoutInput
	if err := decodeJSON(c, &in); err != nil {
		return in, err
	}
	in.AttendeeName = strings.TrimSpace(in.AttendeeName)
	in.AttendeeEmail = normalizeEmail(in.AttendeeEmail)
	in.PromoCode = normalizePromoCode(in.PromoCode)
	if in.AttendeeEmail == "" {
		in.AttendeeEmail = accountEmail
	}
	for i := range in.Items {
		in.Items[i].TicketTypeID = strings.ToLower(in.Items[i].TicketTypeID)
	}
	return in, validateCheckout(in)
}

func validateCheckout(in store.CheckoutInput) error {
	fe := fieldErrors{}
	fe.check(len(in.Items) >= 1 && len(in.Items) <= maxCheckoutItems, "items", "Choose from 1 to 10 ticket types.")
	seen := map[string]bool{}
	for _, item := range in.Items {
		fe.check(eventIDPattern.MatchString(item.TicketTypeID), "items", "Choose a ticket type.")
		fe.check(!seen[item.TicketTypeID], "items", "List each ticket type once.")
		fe.check(item.Quantity >= 1 && item.Quantity <= 100, "items", "Choose a quantity from 1 to 100.")
		seen[item.TicketTypeID] = true
	}
	fe.check(len(in.AttendeeName) > 0 && len(in.AttendeeName) <= 200, "attendeeName", "Enter the attendee name (up to 200 characters).")
	fe.check(validEmail(in.AttendeeEmail), "attendeeEmail", "Enter a valid attendee email address.")
	fe.check(len(in.PromoCode) <= 32, "promoCode", "Use up to 32 characters.")
	return fe.err()
}

func (s *Server) checkout(c echo.Context) error {
	id, err := eventID(c)
	if err != nil {
		return err
	}
	u := currentUser(c)
	in, err := decodeCheckout(c, u.Email)
	if err != nil {
		return err
	}
	if u.EmailVerifiedAt == nil {
		return errRegisterUnverified
	}
	order, err := s.db.Checkout(c.Request().Context(), u.ID, id, in)
	if err != nil {
		return err
	}
	return c.JSON(http.StatusCreated, map[string]any{"order": order})
}

func pathID(c echo.Context, name string) (string, error) {
	id := c.Param(name)
	if !eventIDPattern.MatchString(id) {
		return "", errNotFound
	}
	return id, nil
}

func (s *Server) listMyOrders(c echo.Context) error {
	orders, err := s.db.ListUserOrders(c.Request().Context(), currentUser(c).ID)
	if err != nil {
		return err
	}
	if orders == nil {
		orders = []store.Order{}
	}
	return c.JSON(http.StatusOK, map[string]any{"orders": orders})
}

func (s *Server) getMyOrder(c echo.Context) error {
	id, err := pathID(c, "id")
	if err != nil {
		return err
	}
	order, tickets, err := s.db.UserOrder(c.Request().Context(), currentUser(c).ID, id)
	if err != nil {
		return err
	}
	if tickets == nil {
		tickets = []store.Ticket{}
	}
	return c.JSON(http.StatusOK, map[string]any{"order": order, "tickets": tickets})
}

type payInput struct {
	Outcome string `json:"outcome"`
}

func (s *Server) payMyOrder(c echo.Context) error {
	id, err := pathID(c, "id")
	if err != nil {
		return err
	}
	var in payInput
	if err := decodeJSON(c, &in); err != nil {
		return err
	}
	fe := fieldErrors{}
	fe.check(in.Outcome == "succeeded" || in.Outcome == "failed", "outcome", "Choose succeeded or failed.")
	if err := fe.err(); err != nil {
		return err
	}
	order, payment, err := s.db.PayOrder(c.Request().Context(), currentUser(c).ID, id, in.Outcome == "succeeded")
	if err != nil {
		return err
	}
	return c.JSON(http.StatusOK, map[string]any{"order": order, "payment": payment})
}

func (s *Server) cancelMyOrder(c echo.Context) error {
	id, err := pathID(c, "id")
	if err != nil {
		return err
	}
	order, err := s.db.CancelPendingOrder(c.Request().Context(), currentUser(c).ID, id)
	if err != nil {
		return err
	}
	return c.JSON(http.StatusOK, map[string]any{"order": order})
}
