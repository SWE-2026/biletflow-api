package httpapi

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	"github.com/SWE-2026/biletflow-api/internal/store"
	"github.com/labstack/echo/v4"
)

func TestOrganizerRoutesRequireAuthentication(t *testing.T) {
	t.Parallel()
	e := New(nil, nil, Options{NoAuthRateLimit: true})
	tests := []struct{ method, path string }{
		{http.MethodPost, "/api/v1/organizer/profile"},
		{http.MethodGet, "/api/v1/organizer/profile"},
		{http.MethodGet, "/api/v1/organizer/events"},
		{http.MethodPost, "/api/v1/organizer/events"},
		{http.MethodPost, "/api/v1/organizer/events/00000000-0000-0000-0000-000000000010/publish"},
	}
	for _, tt := range tests {
		t.Run(tt.method+" "+tt.path, func(t *testing.T) {
			t.Parallel()
			rec := httptest.NewRecorder()
			e.ServeHTTP(rec, httptest.NewRequest(tt.method, tt.path, nil))
			if rec.Code != http.StatusUnauthorized {
				t.Fatalf("status = %d, want 401; body %s", rec.Code, rec.Body.String())
			}
		})
	}
}

func TestValidateEvent(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name      string
		give      store.EventInput
		wantError bool
	}{
		{name: "valid", give: eventInput("public")},
		{name: "bad visibility", give: eventInput("unknown"), wantError: true},
		{name: "bad timezone", give: func() store.EventInput { e := eventInput("public"); e.Timezone = "Mars/Olympus"; return e }(), wantError: true},
		{name: "bad date", give: func() store.EventInput { e := eventInput("public"); e.EndsAt = e.StartsAt; return e }(), wantError: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			if got := validateEvent(tt.give); (got != nil) != tt.wantError {
				t.Fatalf("validateEvent() error = %v, wantError %v", got, tt.wantError)
			}
		})
	}
}

func organizerInput(name string) store.OrganizerInput {
	return store.OrganizerInput{Type: "individual", Name: name,
		ContactEmail: "organizer@example.com", ContactPhone: "+7 777 000 0000"}
}

func eventInput(visibility string) store.EventInput {
	start := time.Now().UTC().Add(72 * time.Hour).Truncate(time.Second)
	return store.EventInput{
		Title: "BiletFlow Demo", Description: "Event description", Category: "Community",
		VenueName: "Central Hall", VenueAddress: "10 Main Street, Astana",
		Timezone: "Asia/Almaty", Visibility: visibility, Capacity: 100,
		StartsAt: start, EndsAt: start.Add(2 * time.Hour),
	}
}

func createOrganizerClient(t *testing.T, e *echo.Echo, name string) client {
	t.Helper()
	c := signInWithCode(t, e, uniqueEmail(name))
	c.do(http.MethodGet, "/api/v1/organizer/events", nil, http.StatusForbidden, nil)
	var result struct {
		Organizer store.Organizer `json:"organizer"`
		User      userResponse    `json:"user"`
	}
	c.do(http.MethodPost, "/api/v1/organizer/profile", organizerInput(name), http.StatusCreated, &result)
	if result.Organizer.ID == "" || result.User.Role != "organizer" || result.User.OrganizerID == nil {
		t.Fatalf("organizer creation did not promote user: %+v", result)
	}
	return c
}

func TestOrganizerProfileAndEventLifecycle(t *testing.T) {
	e := newDBServer(t)
	anonymous := client{t: t, e: e}
	anonymous.do(http.MethodGet, "/api/v1/organizer/events", nil, http.StatusUnauthorized, nil)

	// Password sign-up is not verified, so it cannot create an organizer profile yet.
	var signup sessionResponse
	anonymous.do(http.MethodPost, "/api/v1/auth/signup", map[string]string{
		"email": uniqueEmail("unverified"), "password": "correct horse", "displayName": "New User",
	}, http.StatusCreated, &signup)
	unverified := client{t: t, e: e, token: signup.Token}
	unverified.do(http.MethodPost, "/api/v1/organizer/profile", organizerInput("New"), http.StatusForbidden, nil)
	unverified.do(http.MethodPost, "/api/v1/organizer/events", eventInput("public"), http.StatusForbidden, nil)

	owner := createOrganizerClient(t, e, "owner")
	owner.do(http.MethodPost, "/api/v1/organizer/profile", organizerInput("Duplicate"), http.StatusConflict, nil)
	owner.do(http.MethodGet, "/api/v1/organizer/profile", nil, http.StatusOK, nil)
	owner.do(http.MethodPut, "/api/v1/organizer/profile", organizerInput("Updated"), http.StatusOK, nil)

	var created struct {
		Event store.Event `json:"event"`
	}
	owner.do(http.MethodPost, "/api/v1/organizer/events", eventInput("public"), http.StatusCreated, &created)
	if created.Event.ID == "" || created.Event.Status != "draft" || created.Event.Title != "BiletFlow Demo" {
		t.Fatalf("unexpected draft event: %+v", created.Event)
	}
	path := "/api/v1/organizer/events/" + created.Event.ID
	owner.do(http.MethodGet, path, nil, http.StatusOK, nil)
	owner.do(http.MethodGet, "/api/v1/organizer/events", nil, http.StatusOK, nil)

	updated := eventInput("public")
	updated.Title = "Updated event"
	owner.do(http.MethodPut, path, updated, http.StatusOK, nil)
	var published struct {
		Event store.Event `json:"event"`
	}
	owner.do(http.MethodPost, path+"/publish", nil, http.StatusOK, &published)
	if published.Event.Status != "published" || published.Event.Title != updated.Title {
		t.Fatalf("publish lost edits: %+v", published.Event)
	}
	owner.do(http.MethodPost, path+"/publish", nil, http.StatusConflict, nil)
	owner.do(http.MethodPost, path+"/unpublish", nil, http.StatusOK, nil)
	owner.do(http.MethodPost, path+"/cancel", nil, http.StatusOK, nil)
	owner.do(http.MethodPut, path, updated, http.StatusConflict, nil)
	owner.do(http.MethodPost, path+"/publish", nil, http.StatusConflict, nil)
}

func TestOrganizerIsolationAndPublicVisibility(t *testing.T) {
	e := newDBServer(t)
	first := createOrganizerClient(t, e, "first")
	second := createOrganizerClient(t, e, "second")
	var created struct {
		Event store.Event `json:"event"`
	}
	first.do(http.MethodPost, "/api/v1/organizer/events", eventInput("private"), http.StatusCreated, &created)
	path := "/api/v1/organizer/events/" + created.Event.ID
	second.do(http.MethodGet, path, nil, http.StatusNotFound, nil)
	second.do(http.MethodPut, path, eventInput("public"), http.StatusNotFound, nil)
	second.do(http.MethodPost, path+"/publish", nil, http.StatusNotFound, nil)
	second.do(http.MethodPost, path+"/cancel", nil, http.StatusNotFound, nil)
	first.do(http.MethodPost, path+"/publish", nil, http.StatusOK, nil)
	var visible struct {
		Event store.Event `json:"event"`
	}
	first.do(http.MethodPost, "/api/v1/organizer/events", eventInput("public"), http.StatusCreated, &visible)
	first.do(http.MethodPost, "/api/v1/organizer/events/"+visible.Event.ID+"/publish", nil, http.StatusOK, nil)

	var public struct {
		Events []store.Event `json:"events"`
	}
	client{t: t, e: e}.do(http.MethodGet, "/api/v1/events", nil, http.StatusOK, &public)
	foundVisible := false
	for _, event := range public.Events {
		if event.ID == created.Event.ID {
			t.Fatal("private event appeared in public discovery")
		}
		if event.ID == visible.Event.ID {
			foundVisible = true
		}
	}
	if !foundVisible {
		t.Fatal("published public event missing from discovery")
	}
	var others struct {
		Events []store.Event `json:"events"`
	}
	second.do(http.MethodGet, "/api/v1/organizer/events", nil, http.StatusOK, &others)
	for _, event := range others.Events {
		if event.ID == created.Event.ID {
			t.Fatal("another organizer's event appeared in list")
		}
	}
}

func TestOrganizerProfileCreationIsAtomic(t *testing.T) {
	e := newDBServer(t)
	c := signInWithCode(t, e, uniqueEmail("concurrent"))
	body, err := json.Marshal(organizerInput("Concurrent"))
	if err != nil {
		t.Fatal(err)
	}
	statuses := make(chan int, 2)
	var workers sync.WaitGroup
	for range 2 {
		workers.Add(1)
		go func() {
			defer workers.Done()
			req := httptest.NewRequest(http.MethodPost, "/api/v1/organizer/profile", bytes.NewReader(body))
			req.Header.Set(echo.HeaderAuthorization, "Bearer "+c.token)
			req.Header.Set(echo.HeaderContentType, echo.MIMEApplicationJSON)
			rec := httptest.NewRecorder()
			e.ServeHTTP(rec, req)
			statuses <- rec.Code
		}()
	}
	workers.Wait()
	close(statuses)
	counts := map[int]int{}
	for status := range statuses {
		counts[status]++
	}
	if counts[http.StatusCreated] != 1 || counts[http.StatusConflict] != 1 {
		t.Fatalf("concurrent profile creation statuses = %v, want one 201 and one 409", counts)
	}
}

func TestOrganizerEventValidation(t *testing.T) {
	e := newDBServer(t)
	owner := createOrganizerClient(t, e, "validation")
	bad := eventInput("unknown")
	bad.Capacity = 0
	bad.EndsAt = bad.StartsAt.Add(-time.Hour)
	var result Error
	owner.do(http.MethodPost, "/api/v1/organizer/events", bad, http.StatusBadRequest, &result)
	if result.Code != "validation_failed" || len(result.Fields) < 3 {
		t.Fatalf("expected field errors, got %+v", result)
	}
	owner.do(http.MethodGet, "/api/v1/organizer/events/not-a-uuid", nil, http.StatusNotFound, nil)
}
