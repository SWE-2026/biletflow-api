package httpapi

import (
	"net/http"
	"strings"

	"github.com/labstack/echo/v4"
)

var payoutStatuses = []string{"scheduled", "on_hold", "paid"}

func (s *Server) listOrganizerPayouts(c echo.Context) error {
	payouts, err := s.db.OrganizerPayouts(c.Request().Context(), organizerID(c))
	return listJSON(c, "payouts", payouts, err)
}

func (s *Server) adminPayouts(c echo.Context) error {
	_, status, err := searchParams(c, payoutStatuses)
	if err != nil {
		return err
	}
	payouts, err := s.db.AdminPayouts(c.Request().Context(), status)
	return listJSON(c, "payouts", payouts, err)
}

func (s *Server) schedulePayouts(c echo.Context) error {
	payouts, err := s.db.SchedulePayouts(c.Request().Context(), currentUser(c).ID)
	return listJSON(c, "payouts", payouts, err)
}

func (s *Server) markPayoutPaid(c echo.Context) error { return s.setPayoutStatus(c, "paid") }

func (s *Server) holdPayout(c echo.Context) error { return s.setPayoutStatus(c, "on_hold") }

func (s *Server) releasePayout(c echo.Context) error { return s.setPayoutStatus(c, "scheduled") }

func (s *Server) setPayoutStatus(c echo.Context, status string) error {
	payoutID, err := pathID(c, "payoutId")
	if err != nil {
		return err
	}
	po, err := s.db.SetPayoutStatus(c.Request().Context(), currentUser(c).ID, payoutID, status)
	if err != nil {
		return err
	}
	return c.JSON(http.StatusOK, map[string]any{"payout": po})
}

type reviewInput struct {
	Note string `json:"note"`
}

func (s *Server) approvePaidSales(c echo.Context) error { return s.reviewPaidSales(c, true) }

func (s *Server) rejectPaidSales(c echo.Context) error { return s.reviewPaidSales(c, false) }

func (s *Server) reviewPaidSales(c echo.Context, approve bool) error {
	id, err := eventID(c)
	if err != nil {
		return err
	}
	var in reviewInput
	if err := decodeJSON(c, &in); err != nil {
		return err
	}
	in.Note = strings.TrimSpace(in.Note)
	fe := fieldErrors{}
	fe.check(len(in.Note) <= 500, "note", "Use up to 500 characters.")
	fe.check(approve || in.Note != "", "note", "Explain why the activation is rejected.")
	if err := fe.err(); err != nil {
		return err
	}
	if err := s.db.ReviewPaidSales(c.Request().Context(), currentUser(c).ID, id, approve, in.Note); err != nil {
		return err
	}
	return c.NoContent(http.StatusNoContent)
}
