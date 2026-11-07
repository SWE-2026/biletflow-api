package httpapi

import (
	"net/http"
	"slices"
	"strings"

	"github.com/SWE-2026/biletflow-api/internal/store"
	"github.com/labstack/echo/v4"
)

var (
	attendeeCategories  = []string{"ticket_delivery", "payment", "refund", "seating", "event_information", "check_in", "account", "technical", "other"}
	organizerCategories = []string{"account", "activation", "payment", "technical", "other"}
	supportStatuses     = []string{"open", "in_progress", "waiting_for_customer", "resolved"}
)

func supportViewer(c echo.Context) store.SupportViewer {
	u := currentUser(c)
	v := store.SupportViewer{UserID: u.ID, Admin: u.Role == "admin"}
	if u.OrganizerID != nil {
		v.OrganizerID = *u.OrganizerID
	}
	return v
}

func validID(id *string) bool { return id == nil || eventIDPattern.MatchString(*id) }

func lowerID(id *string) {
	if id != nil {
		*id = strings.ToLower(*id)
	}
}

func decodeSupportCase(c echo.Context) (store.SupportCaseInput, error) {
	var in store.SupportCaseInput
	if err := decodeJSON(c, &in); err != nil {
		return in, err
	}
	in.Subject = strings.TrimSpace(in.Subject)
	in.Message = strings.TrimSpace(in.Message)
	lowerID(in.EventID)
	lowerID(in.OrderID)
	lowerID(in.TicketID)
	return in, validateSupportCase(in)
}

func validateSupportCase(in store.SupportCaseInput) error {
	fe := fieldErrors{}
	fe.check(in.Kind == "attendee" || in.Kind == "organizer", "kind", "Choose attendee or organizer.")
	categories := attendeeCategories
	if in.Kind == "organizer" {
		categories = organizerCategories
	}
	fe.check(slices.Contains(categories, in.Category), "category", "Choose a category for this kind of case.")
	fe.check(len(in.Subject) > 0 && len(in.Subject) <= 200, "subject", "Enter a subject (up to 200 characters).")
	fe.check(len(in.Message) > 0 && len(in.Message) <= 5000, "message", "Enter a message (up to 5000 characters).")
	fe.check(validID(in.EventID), "eventId", "Choose an event.")
	fe.check(validID(in.OrderID), "orderId", "Choose an order.")
	fe.check(validID(in.TicketID), "ticketId", "Choose a ticket.")
	links := 0
	for _, id := range []*string{in.EventID, in.OrderID, in.TicketID} {
		if id != nil {
			links++
		}
	}
	if in.Kind == "attendee" {
		fe.check(links == 1, "eventId", "Link exactly one event, order or ticket.")
	} else {
		fe.check(in.OrderID == nil && in.TicketID == nil, "eventId", "Organizer cases can link an event only.")
	}
	return fe.err()
}

func (s *Server) createSupportCase(c echo.Context) error {
	in, err := decodeSupportCase(c)
	if err != nil {
		return err
	}
	sc, err := s.db.CreateSupportCase(c.Request().Context(), supportViewer(c), in)
	if err != nil {
		return err
	}
	return c.JSON(http.StatusCreated, map[string]any{"case": sc})
}

func (s *Server) listSupportCases(c echo.Context) error {
	status := c.QueryParam("status")
	eventFilter := strings.ToLower(c.QueryParam("eventId"))
	fe := fieldErrors{}
	fe.check(status == "" || slices.Contains(supportStatuses, status), "status", "Choose a valid status.")
	fe.check(eventFilter == "" || eventIDPattern.MatchString(eventFilter), "eventId", "Choose an event.")
	if err := fe.err(); err != nil {
		return err
	}
	cases, err := s.db.ListSupportCases(c.Request().Context(), supportViewer(c), status, eventFilter)
	if err != nil {
		return err
	}
	if cases == nil {
		cases = []store.SupportCase{}
	}
	return c.JSON(http.StatusOK, map[string]any{"cases": cases})
}

func (s *Server) unreadSupportCases(c echo.Context) error {
	n, err := s.db.UnreadSupportCases(c.Request().Context(), supportViewer(c))
	if err != nil {
		return err
	}
	return c.JSON(http.StatusOK, map[string]any{"unread": n})
}

func (s *Server) getSupportCase(c echo.Context) error {
	id, err := pathID(c, "id")
	if err != nil {
		return err
	}
	sc, messages, err := s.db.SupportCase(c.Request().Context(), supportViewer(c), id)
	if err != nil {
		return err
	}
	if messages == nil {
		messages = []store.SupportMessage{}
	}
	return c.JSON(http.StatusOK, map[string]any{"case": sc, "messages": messages})
}

type supportMessageInput struct {
	Body string `json:"body"`
}

func (s *Server) postSupportMessage(c echo.Context) error {
	id, err := pathID(c, "id")
	if err != nil {
		return err
	}
	var in supportMessageInput
	if err := decodeJSON(c, &in); err != nil {
		return err
	}
	in.Body = strings.TrimSpace(in.Body)
	fe := fieldErrors{}
	fe.check(len(in.Body) > 0 && len(in.Body) <= 5000, "body", "Enter a message (up to 5000 characters).")
	if err := fe.err(); err != nil {
		return err
	}
	sc, err := s.db.AddSupportMessage(c.Request().Context(), supportViewer(c), id, in.Body)
	if err != nil {
		return err
	}
	return c.JSON(http.StatusCreated, map[string]any{"case": sc})
}

type supportStatusInput struct {
	Status string `json:"status"`
}

func (s *Server) setSupportStatus(c echo.Context) error {
	id, err := pathID(c, "id")
	if err != nil {
		return err
	}
	var in supportStatusInput
	if err := decodeJSON(c, &in); err != nil {
		return err
	}
	fe := fieldErrors{}
	fe.check(slices.Contains(supportStatuses, in.Status), "status", "Choose a valid status.")
	if err := fe.err(); err != nil {
		return err
	}
	sc, err := s.db.SetSupportStatus(c.Request().Context(), supportViewer(c), id, in.Status)
	if err != nil {
		return err
	}
	return c.JSON(http.StatusOK, map[string]any{"case": sc})
}

type supportAssignInput struct {
	AssigneeID *string `json:"assigneeId"`
}

func (s *Server) assignSupportCase(c echo.Context) error {
	id, err := pathID(c, "id")
	if err != nil {
		return err
	}
	var in supportAssignInput
	if err := decodeJSON(c, &in); err != nil {
		return err
	}
	lowerID(in.AssigneeID)
	fe := fieldErrors{}
	fe.check(validID(in.AssigneeID), "assigneeId", "Choose a user or null.")
	if err := fe.err(); err != nil {
		return err
	}
	sc, err := s.db.AssignSupportCase(c.Request().Context(), supportViewer(c), id, in.AssigneeID)
	if err != nil {
		return err
	}
	return c.JSON(http.StatusOK, map[string]any{"case": sc})
}
