package httpapi

import (
	"net/http"
	"slices"
	"strings"
	"time"

	"github.com/SWE-2026/biletflow-api/internal/store"
	"github.com/labstack/echo/v4"
)

var ticketStatuses = []string{"valid", "used", "cancelled", "refunded"}

func timeParam(c echo.Context, fe fieldErrors, name string) *time.Time {
	raw := c.QueryParam(name)
	if raw == "" {
		return nil
	}
	t, err := time.Parse(time.RFC3339, raw)
	fe.check(err == nil, name, "Use an RFC 3339 time.")
	if err != nil {
		return nil
	}
	return &t
}

func dateRange(c echo.Context, fe fieldErrors) (*time.Time, *time.Time) {
	from, to := timeParam(c, fe, "from"), timeParam(c, fe, "to")
	fe.check(from == nil || to == nil || to.After(*from), "to", "End must follow start.")
	return from, to
}

func ticketTypeParam(c echo.Context, fe fieldErrors) string {
	id := strings.ToLower(c.QueryParam("ticketTypeId"))
	fe.check(id == "" || eventIDPattern.MatchString(id), "ticketTypeId", "Choose a ticket type.")
	return id
}

func (s *Server) getEventAnalytics(c echo.Context) error {
	id, err := eventID(c)
	if err != nil {
		return err
	}
	fe := fieldErrors{}
	var f store.AnalyticsFilter
	f.From, f.To = dateRange(c, fe)
	f.TicketTypeID = ticketTypeParam(c, fe)
	if err := fe.err(); err != nil {
		return err
	}
	a, err := s.db.EventAnalytics(c.Request().Context(), organizerID(c), id, f)
	if err != nil {
		return err
	}
	if a.Campaigns == nil {
		a.Campaigns = []store.Campaign{}
	}
	return c.JSON(http.StatusOK, map[string]any{"analytics": a})
}

func (s *Server) listEventActivity(c echo.Context) error {
	id, err := eventID(c)
	if err != nil {
		return err
	}
	fe := fieldErrors{}
	var f store.ActivityFilter
	f.From, f.To = dateRange(c, fe)
	f.Type = c.QueryParam("type")
	fe.check(f.Type == "" || slices.Contains(store.ActivityTypes, f.Type), "type", "Choose a valid activity type.")
	if err := fe.err(); err != nil {
		return err
	}
	entries, err := s.db.ListActivity(c.Request().Context(), organizerID(c), id, f)
	if err != nil {
		return err
	}
	if entries == nil {
		entries = []store.ActivityEntry{}
	}
	return c.JSON(http.StatusOK, map[string]any{"activity": entries})
}

func (s *Server) listEventAttendees(c echo.Context) error {
	id, err := eventID(c)
	if err != nil {
		return err
	}
	fe := fieldErrors{}
	f := store.AttendeeFilter{TicketTypeID: ticketTypeParam(c, fe), Status: c.QueryParam("status"),
		Query: strings.TrimSpace(c.QueryParam("q"))}
	fe.check(f.Status == "" || slices.Contains(ticketStatuses, f.Status), "status", "Choose a valid ticket status.")
	fe.check(len(f.Query) <= 100, "q", "Use up to 100 characters.")
	if err := fe.err(); err != nil {
		return err
	}
	tickets, err := s.db.ListEventAttendees(c.Request().Context(), organizerID(c), id, f)
	if err != nil {
		return err
	}
	if tickets == nil {
		tickets = []store.StaffTicket{}
	}
	return c.JSON(http.StatusOK, map[string]any{"tickets": tickets})
}

func (s *Server) duplicateEvent(c echo.Context) error {
	id, err := eventID(c)
	if err != nil {
		return err
	}
	e, err := s.db.DuplicateEvent(c.Request().Context(), organizerID(c), currentUser(c).ID, id)
	if err != nil {
		return err
	}
	return c.JSON(http.StatusCreated, map[string]any{"event": e})
}
