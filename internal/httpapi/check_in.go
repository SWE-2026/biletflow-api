package httpapi

import (
	"net/http"
	"strings"

	"github.com/SWE-2026/biletflow-api/internal/store"
	"github.com/labstack/echo/v4"
)

type staffInput struct {
	Email string `json:"email"`
	Role  string `json:"role"`
}

type scanInput struct {
	QRToken string `json:"qrToken"`
}

func (s *Server) listStaff(c echo.Context) error {
	id, err := eventID(c)
	if err != nil {
		return err
	}
	staff, err := s.db.ListStaff(c.Request().Context(), organizerID(c), id)
	if err != nil {
		return err
	}
	if staff == nil {
		staff = []store.StaffMember{}
	}
	return c.JSON(http.StatusOK, map[string]any{"staff": staff})
}

func (s *Server) assignStaff(c echo.Context) error {
	id, err := eventID(c)
	if err != nil {
		return err
	}
	var in staffInput
	if err := decodeJSON(c, &in); err != nil {
		return err
	}
	in.Email = normalizeEmail(in.Email)
	fe := fieldErrors{}
	fe.check(validEmail(in.Email), "email", "Enter a valid email address.")
	fe.check(in.Role == "scanner" || in.Role == "supervisor", "role", "Choose scanner or supervisor.")
	if err := fe.err(); err != nil {
		return err
	}
	m, err := s.db.AssignStaff(c.Request().Context(), organizerID(c), currentUser(c).ID, id, in.Email, in.Role)
	if err != nil {
		return err
	}
	return c.JSON(http.StatusOK, map[string]any{"staffMember": m})
}

func (s *Server) removeStaff(c echo.Context) error {
	id, err := eventID(c)
	if err != nil {
		return err
	}
	userID, err := pathID(c, "userId")
	if err != nil {
		return err
	}
	if err := s.db.RemoveStaff(c.Request().Context(), organizerID(c), currentUser(c).ID, id, userID); err != nil {
		return err
	}
	return c.NoContent(http.StatusNoContent)
}

func (s *Server) listStaffEvents(c echo.Context) error {
	events, err := s.db.ListStaffEvents(c.Request().Context(), currentUser(c).ID)
	if err != nil {
		return err
	}
	if events == nil {
		events = []store.Event{}
	}
	return c.JSON(http.StatusOK, map[string]any{"events": events})
}

func (s *Server) getStaffEvent(c echo.Context) error {
	id, err := eventID(c)
	if err != nil {
		return err
	}
	event, role, summary, err := s.db.StaffEvent(c.Request().Context(), currentUser(c).ID, id)
	if err != nil {
		return err
	}
	return c.JSON(http.StatusOK, map[string]any{"event": event, "role": role, "summary": summary})
}

func (s *Server) searchAttendees(c echo.Context) error {
	id, err := eventID(c)
	if err != nil {
		return err
	}
	query := strings.TrimSpace(c.QueryParam("q"))
	fe := fieldErrors{}
	fe.check(len(query) <= 100, "q", "Use up to 100 characters.")
	if err := fe.err(); err != nil {
		return err
	}
	tickets, err := s.db.SearchAttendees(c.Request().Context(), currentUser(c).ID, id, query)
	if err != nil {
		return err
	}
	if tickets == nil {
		tickets = []store.StaffTicket{}
	}
	return c.JSON(http.StatusOK, map[string]any{"tickets": tickets})
}

func (s *Server) checkInByToken(c echo.Context) error {
	id, err := eventID(c)
	if err != nil {
		return err
	}
	var in scanInput
	if err := decodeJSON(c, &in); err != nil {
		return err
	}
	in.QRToken = strings.TrimSpace(in.QRToken)
	fe := fieldErrors{}
	fe.check(len(in.QRToken) > 0 && len(in.QRToken) <= 128, "qrToken", "Scan a ticket QR code.")
	if err := fe.err(); err != nil {
		return err
	}
	res, err := s.db.CheckInByToken(c.Request().Context(), currentUser(c).ID, id, in.QRToken)
	if err != nil {
		return err
	}
	return c.JSON(http.StatusOK, res)
}

func staffTicketPath(c echo.Context) (string, string, error) {
	id, err := eventID(c)
	if err != nil {
		return "", "", err
	}
	ticketID, err := pathID(c, "ticketId")
	return id, ticketID, err
}

func (s *Server) checkInByID(c echo.Context) error {
	id, ticketID, err := staffTicketPath(c)
	if err != nil {
		return err
	}
	res, err := s.db.CheckInByID(c.Request().Context(), currentUser(c).ID, id, ticketID)
	if err != nil {
		return err
	}
	return c.JSON(http.StatusOK, res)
}

func (s *Server) undoCheckIn(c echo.Context) error {
	id, ticketID, err := staffTicketPath(c)
	if err != nil {
		return err
	}
	ticket, err := s.db.UndoCheckIn(c.Request().Context(), currentUser(c).ID, id, ticketID)
	if err != nil {
		return err
	}
	return c.JSON(http.StatusOK, map[string]any{"ticket": ticket})
}
