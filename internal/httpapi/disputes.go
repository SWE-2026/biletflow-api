package httpapi

import (
	"net/http"
	"slices"
	"strings"

	"github.com/labstack/echo/v4"
)

var (
	disputeReasons  = []string{"not_received", "event_cancelled", "not_as_described", "duplicate_charge", "other"}
	disputeStatuses = []string{"open", "refunded", "rejected"}
)

type disputeInput struct {
	Reason  string `json:"reason"`
	Details string `json:"details"`
}

func (s *Server) openDispute(c echo.Context) error {
	orderID, err := pathID(c, "id")
	if err != nil {
		return err
	}
	var in disputeInput
	if err := decodeJSON(c, &in); err != nil {
		return err
	}
	in.Details = strings.TrimSpace(in.Details)
	fe := fieldErrors{}
	fe.check(slices.Contains(disputeReasons, in.Reason), "reason", "Choose a dispute reason.")
	fe.check(len(in.Details) <= 2000, "details", "Use up to 2000 characters.")
	if err := fe.err(); err != nil {
		return err
	}
	d, err := s.db.OpenDispute(c.Request().Context(), currentUser(c).ID, orderID, in.Reason, in.Details)
	if err != nil {
		return err
	}
	return c.JSON(http.StatusCreated, map[string]any{"dispute": d})
}

func (s *Server) listMyDisputes(c echo.Context) error {
	disputes, err := s.db.UserDisputes(c.Request().Context(), currentUser(c).ID)
	return listJSON(c, "disputes", disputes, err)
}

func (s *Server) listEventDisputes(c echo.Context) error {
	id, err := eventID(c)
	if err != nil {
		return err
	}
	disputes, err := s.db.EventDisputes(c.Request().Context(), organizerID(c), id)
	return listJSON(c, "disputes", disputes, err)
}

func (s *Server) adminDisputes(c echo.Context) error {
	_, status, err := searchParams(c, disputeStatuses)
	if err != nil {
		return err
	}
	disputes, err := s.db.AdminDisputes(c.Request().Context(), status)
	return listJSON(c, "disputes", disputes, err)
}

type resolveDisputeInput struct {
	Decision string `json:"decision"`
	Note     string `json:"note"`
}

func (s *Server) resolveDispute(c echo.Context) error {
	disputeID, err := pathID(c, "disputeId")
	if err != nil {
		return err
	}
	var in resolveDisputeInput
	if err := decodeJSON(c, &in); err != nil {
		return err
	}
	in.Note = strings.TrimSpace(in.Note)
	fe := fieldErrors{}
	fe.check(in.Decision == "refund" || in.Decision == "reject", "decision", "Choose refund or reject.")
	fe.check(len(in.Note) > 0 && len(in.Note) <= 500, "note", "Explain the decision (up to 500 characters).")
	if err := fe.err(); err != nil {
		return err
	}
	d, err := s.db.ResolveDispute(c.Request().Context(), currentUser(c).ID, disputeID, in.Decision == "refund", in.Note)
	if err != nil {
		return err
	}
	return c.JSON(http.StatusOK, map[string]any{"dispute": d})
}
