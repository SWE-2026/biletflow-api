package httpapi

import (
	"net/http"
	"strings"

	"github.com/SWE-2026/biletflow-api/internal/store"
	"github.com/labstack/echo/v4"
)

type activationInput struct {
	AcceptTerms bool `json:"acceptTerms"`
}

type refundInput struct {
	Reason string `json:"reason"`
}

func (s *Server) getPaidSales(c echo.Context) error {
	id, err := eventID(c)
	if err != nil {
		return err
	}
	ps, err := s.db.PaidSales(c.Request().Context(), organizerID(c), id)
	if err != nil {
		return err
	}
	return c.JSON(http.StatusOK, map[string]any{"paidSales": ps})
}

func (s *Server) activatePaidSales(c echo.Context) error {
	id, err := eventID(c)
	if err != nil {
		return err
	}
	var in activationInput
	if err := decodeJSON(c, &in); err != nil {
		return err
	}
	fe := fieldErrors{}
	fe.check(in.AcceptTerms, "acceptTerms", "Accept the paid-event terms to continue.")
	if err := fe.err(); err != nil {
		return err
	}
	ps, err := s.db.RequestPaidSales(c.Request().Context(), organizerID(c), currentUser(c).ID, id)
	if err != nil {
		return err
	}
	return c.JSON(http.StatusOK, map[string]any{"paidSales": ps})
}

func (s *Server) getEventSales(c echo.Context) error {
	id, err := eventID(c)
	if err != nil {
		return err
	}
	sales, err := s.db.EventSales(c.Request().Context(), organizerID(c), id)
	if err != nil {
		return err
	}
	return c.JSON(http.StatusOK, map[string]any{"sales": sales})
}

func (s *Server) listEventOrders(c echo.Context) error {
	id, err := eventID(c)
	if err != nil {
		return err
	}
	orders, err := s.db.ListEventOrders(c.Request().Context(), organizerID(c), id)
	if err != nil {
		return err
	}
	if orders == nil {
		orders = []store.OrganizerOrder{}
	}
	return c.JSON(http.StatusOK, map[string]any{"orders": orders})
}

func eventOrderPath(c echo.Context) (string, string, error) {
	id, err := eventID(c)
	if err != nil {
		return "", "", err
	}
	orderID, err := pathID(c, "orderId")
	return id, orderID, err
}

func (s *Server) refundOrder(c echo.Context) error {
	id, orderID, err := eventOrderPath(c)
	if err != nil {
		return err
	}
	var in refundInput
	if err := decodeJSON(c, &in); err != nil {
		return err
	}
	in.Reason = strings.TrimSpace(in.Reason)
	fe := fieldErrors{}
	fe.check(len(in.Reason) <= 500, "reason", "Use up to 500 characters.")
	if err := fe.err(); err != nil {
		return err
	}
	order, err := s.db.RefundOrder(c.Request().Context(), organizerID(c), currentUser(c).ID, id, orderID, in.Reason)
	if err != nil {
		return err
	}
	return c.JSON(http.StatusOK, map[string]any{"order": order})
}

func (s *Server) cancelRegistration(c echo.Context) error {
	id, orderID, err := eventOrderPath(c)
	if err != nil {
		return err
	}
	order, err := s.db.CancelRegistration(c.Request().Context(), organizerID(c), currentUser(c).ID, id, orderID)
	if err != nil {
		return err
	}
	return c.JSON(http.StatusOK, map[string]any{"order": order})
}
