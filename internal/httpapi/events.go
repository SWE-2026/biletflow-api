package httpapi

import (
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"time"

	"github.com/SWE-2026/biletflow-api/internal/store"
	"github.com/labstack/echo/v4"
)

var eventIDPattern = regexp.MustCompile(`^[0-9a-fA-F]{8}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{12}$`)

func validImageURL(raw *string) bool {
	if raw == nil {
		return true
	}
	if len(*raw) == 0 || len(*raw) > 2048 {
		return false
	}
	u, err := url.Parse(*raw)
	return err == nil && (u.Scheme == "https" || u.Scheme == "http") && u.Host != "" && u.User == nil
}

func decodeEvent(c echo.Context) (store.EventInput, error) {
	var in store.EventInput
	if err := decodeJSON(c, &in); err != nil {
		return in, err
	}
	in.Title = strings.TrimSpace(in.Title)
	in.Description = strings.TrimSpace(in.Description)
	in.Category = strings.TrimSpace(in.Category)
	in.VenueName = strings.TrimSpace(in.VenueName)
	in.VenueAddress = strings.TrimSpace(in.VenueAddress)
	in.Timezone = strings.TrimSpace(in.Timezone)
	if in.ImageURL != nil {
		image := strings.TrimSpace(*in.ImageURL)
		in.ImageURL = &image
	}
	return in, validateEvent(in)
}

func validateEvent(in store.EventInput) error {
	fe := fieldErrors{}
	fe.check(len(in.Title) > 0 && len(in.Title) <= 200, "title", "Enter a title (up to 200 characters).")
	fe.check(len(in.Description) <= 10000, "description", "Use up to 10000 characters.")
	fe.check(len(in.Category) > 0 && len(in.Category) <= 100, "category", "Enter a category (up to 100 characters).")
	fe.check(validImageURL(in.ImageURL), "imageUrl", "Enter an http or https image URL.")
	fe.check(len(in.VenueName) > 0 && len(in.VenueName) <= 200, "venueName", "Enter a venue name (up to 200 characters).")
	fe.check(len(in.VenueAddress) > 0 && len(in.VenueAddress) <= 500, "venueAddress", "Enter a venue address (up to 500 characters).")
	_, zoneErr := time.LoadLocation(in.Timezone)
	fe.check(in.Timezone != "" && zoneErr == nil, "timezone", "Enter a valid IANA timezone.")
	fe.check(in.Visibility == "public" || in.Visibility == "unlisted" || in.Visibility == "private", "visibility", "Choose public, unlisted or private.")
	fe.check(in.Capacity > 0 && in.Capacity <= 1000000, "capacity", "Choose a capacity from 1 to 1000000.")
	fe.check(!in.StartsAt.IsZero() && in.EndsAt.After(in.StartsAt), "endsAt", "End time must be after start time.")
	fe.check(in.RegistrationOpensAt == nil || in.RegistrationClosesAt == nil || in.RegistrationClosesAt.After(*in.RegistrationOpensAt), "registrationClosesAt", "Registration close must follow open.")
	fe.check(in.RegistrationOpensAt == nil || in.RegistrationOpensAt.Before(in.StartsAt), "registrationOpensAt", "Registration must open before the event starts.")
	fe.check(in.RegistrationClosesAt == nil || in.RegistrationClosesAt.Before(in.StartsAt), "registrationClosesAt", "Registration must close before the event starts.")
	return fe.err()
}

func eventID(c echo.Context) (string, error) {
	id := c.Param("id")
	if !eventIDPattern.MatchString(id) {
		return "", errNotFound
	}
	return id, nil
}

func (s *Server) createEvent(c echo.Context) error {
	in, err := decodeEvent(c)
	if err != nil {
		return err
	}
	e, err := s.db.CreateEvent(c.Request().Context(), organizerID(c), in)
	if err != nil {
		return err
	}
	return c.JSON(http.StatusCreated, map[string]any{"event": e})
}

func (s *Server) listOrganizerEvents(c echo.Context) error {
	events, err := s.db.ListOrganizerEvents(c.Request().Context(), organizerID(c))
	if err != nil {
		return err
	}
	if events == nil {
		events = []store.Event{}
	}
	return c.JSON(http.StatusOK, map[string]any{"events": events})
}

func (s *Server) getOrganizerEvent(c echo.Context) error {
	id, err := eventID(c)
	if err != nil {
		return err
	}
	e, err := s.db.EventByOwner(c.Request().Context(), organizerID(c), id)
	if err != nil {
		return err
	}
	return c.JSON(http.StatusOK, map[string]any{"event": e})
}

func (s *Server) updateEvent(c echo.Context) error {
	id, err := eventID(c)
	if err != nil {
		return err
	}
	in, err := decodeEvent(c)
	if err != nil {
		return err
	}
	e, err := s.db.UpdateEvent(c.Request().Context(), organizerID(c), id, in)
	if err != nil {
		return err
	}
	return c.JSON(http.StatusOK, map[string]any{"event": e})
}

func (s *Server) publishEvent(c echo.Context) error {
	return s.transitionEvent(c, "draft", "published")
}

func (s *Server) unpublishEvent(c echo.Context) error {
	return s.transitionEvent(c, "published", "draft")
}

func (s *Server) transitionEvent(c echo.Context, from, to string) error {
	id, err := eventID(c)
	if err != nil {
		return err
	}
	e, err := s.db.TransitionEvent(c.Request().Context(), organizerID(c), id, from, to)
	if err != nil {
		return err
	}
	return c.JSON(http.StatusOK, map[string]any{"event": e})
}

func (s *Server) cancelEvent(c echo.Context) error {
	id, err := eventID(c)
	if err != nil {
		return err
	}
	e, err := s.db.CancelEvent(c.Request().Context(), organizerID(c), id)
	if err != nil {
		return err
	}
	return c.JSON(http.StatusOK, map[string]any{"event": e})
}
