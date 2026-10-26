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

func ticketTypeInput(name string, quantity int) store.TicketTypeInput {
	return store.TicketTypeInput{Name: name, PriceAmount: 0, Quantity: quantity, MaxPerOrder: 10}
}

func TestValidateTicketType(t *testing.T) {
	t.Parallel()
	start := time.Now().UTC()
	end := start.Add(-time.Hour)
	tests := []struct {
		name      string
		give      store.TicketTypeInput
		wantError bool
	}{
		{name: "valid free", give: ticketTypeInput("General", 10)},
		{name: "valid paid", give: func() store.TicketTypeInput { tt := ticketTypeInput("VIP", 5); tt.PriceAmount = 15000; return tt }()},
		{name: "missing name", give: ticketTypeInput("", 10), wantError: true},
		{name: "zero quantity", give: ticketTypeInput("General", 0), wantError: true},
		{name: "negative price", give: func() store.TicketTypeInput { tt := ticketTypeInput("General", 1); tt.PriceAmount = -1; return tt }(), wantError: true},
		{name: "price too high", give: func() store.TicketTypeInput {
			tt := ticketTypeInput("General", 1)
			tt.PriceAmount = maxPriceAmount + 1
			return tt
		}(), wantError: true},
		{name: "zero per order", give: func() store.TicketTypeInput { tt := ticketTypeInput("General", 1); tt.MaxPerOrder = 0; return tt }(), wantError: true},
		{name: "sales end before start", give: func() store.TicketTypeInput {
			tt := ticketTypeInput("General", 1)
			tt.SalesStartsAt, tt.SalesEndsAt = &start, &end
			return tt
		}(), wantError: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			if got := validateTicketType(tt.give); (got != nil) != tt.wantError {
				t.Fatalf("validateTicketType() error = %v, wantError %v", got, tt.wantError)
			}
		})
	}
}

func TestTicketTypeRoutesRequireAuthentication(t *testing.T) {
	t.Parallel()
	e := New(nil, nil, Options{NoAuthRateLimit: true})
	base := "/api/v1/organizer/events/00000000-0000-0000-0000-000000000010/ticket-types"
	tests := []struct{ method, path string }{
		{http.MethodGet, base},
		{http.MethodPost, base},
		{http.MethodPut, base + "/00000000-0000-0000-0000-000000000011"},
		{http.MethodDelete, base + "/00000000-0000-0000-0000-000000000011"},
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

type ticketTypeResponse struct {
	TicketType store.TicketType `json:"ticketType"`
}

type publicEventResponse struct {
	Event       store.Event        `json:"event"`
	TicketTypes []store.TicketType `json:"ticketTypes"`
}

func createEvent(t *testing.T, c client, in store.EventInput) string {
	t.Helper()
	var created struct {
		Event store.Event `json:"event"`
	}
	c.do(http.MethodPost, "/api/v1/organizer/events", in, http.StatusCreated, &created)
	return created.Event.ID
}

func TestTicketTypeLifecycleAndCapacity(t *testing.T) {
	e := newDBServer(t)
	owner := createOrganizerClient(t, e, "tickets")
	event := eventInput("public")
	eventPath := "/api/v1/organizer/events/" + createEvent(t, owner, event)
	typesPath := eventPath + "/ticket-types"

	var general, vip ticketTypeResponse
	owner.do(http.MethodPost, typesPath, ticketTypeInput("General", 60), http.StatusCreated, &general)
	if general.TicketType.ID == "" || general.TicketType.Quantity != 60 || general.TicketType.MaxPerOrder != 10 {
		t.Fatalf("unexpected ticket type: %+v", general.TicketType)
	}
	var result Error
	owner.do(http.MethodPost, typesPath, ticketTypeInput("VIP", 50), http.StatusConflict, &result)
	if result.Code != "capacity_exceeded" {
		t.Fatalf("code = %q, want capacity_exceeded", result.Code)
	}
	owner.do(http.MethodPut, typesPath+"/"+general.TicketType.ID, ticketTypeInput("General", 40), http.StatusOK, nil)
	owner.do(http.MethodPost, typesPath, ticketTypeInput("VIP", 50), http.StatusCreated, &vip)

	// Capacity cannot drop below the 90 tickets already allotted.
	event.Capacity = 80
	owner.do(http.MethodPut, eventPath, event, http.StatusConflict, nil)
	event.Capacity = 90
	owner.do(http.MethodPut, eventPath, event, http.StatusOK, nil)

	var list struct {
		TicketTypes []store.TicketType `json:"ticketTypes"`
	}
	owner.do(http.MethodGet, typesPath, nil, http.StatusOK, &list)
	if len(list.TicketTypes) != 2 {
		t.Fatalf("listed %d ticket types, want 2", len(list.TicketTypes))
	}

	owner.do(http.MethodDelete, typesPath+"/"+vip.TicketType.ID, nil, http.StatusNoContent, nil)
	owner.do(http.MethodDelete, typesPath+"/"+vip.TicketType.ID, nil, http.StatusNotFound, nil)
	owner.do(http.MethodPut, typesPath+"/not-a-uuid", ticketTypeInput("General", 1), http.StatusNotFound, nil)

	owner.do(http.MethodPost, eventPath+"/cancel", nil, http.StatusOK, nil)
	owner.do(http.MethodPost, typesPath, ticketTypeInput("Late", 1), http.StatusConflict, nil)
	owner.do(http.MethodPut, typesPath+"/"+general.TicketType.ID, ticketTypeInput("General", 1), http.StatusConflict, nil)
	owner.do(http.MethodDelete, typesPath+"/"+general.TicketType.ID, nil, http.StatusConflict, nil)
}

func TestTicketTypeIsolation(t *testing.T) {
	e := newDBServer(t)
	owner := createOrganizerClient(t, e, "types-owner")
	other := createOrganizerClient(t, e, "types-other")
	typesPath := "/api/v1/organizer/events/" + createEvent(t, owner, eventInput("public")) + "/ticket-types"
	var created ticketTypeResponse
	owner.do(http.MethodPost, typesPath, ticketTypeInput("General", 10), http.StatusCreated, &created)
	typePath := typesPath + "/" + created.TicketType.ID

	other.do(http.MethodGet, typesPath, nil, http.StatusNotFound, nil)
	other.do(http.MethodPost, typesPath, ticketTypeInput("Stolen", 1), http.StatusNotFound, nil)
	other.do(http.MethodPut, typePath, ticketTypeInput("Stolen", 1), http.StatusNotFound, nil)
	other.do(http.MethodDelete, typePath, nil, http.StatusNotFound, nil)

	// A ticket type ID only resolves under its own event.
	otherTypes := "/api/v1/organizer/events/" + createEvent(t, owner, eventInput("public")) + "/ticket-types"
	owner.do(http.MethodPut, otherTypes+"/"+created.TicketType.ID, ticketTypeInput("Moved", 1), http.StatusNotFound, nil)
	owner.do(http.MethodDelete, otherTypes+"/"+created.TicketType.ID, nil, http.StatusNotFound, nil)
}

func TestPublicEventDetail(t *testing.T) {
	e := newDBServer(t)
	owner := createOrganizerClient(t, e, "public-detail")
	anonymous := client{t: t, e: e}
	eventID := createEvent(t, owner, eventInput("public"))
	eventPath := "/api/v1/organizer/events/" + eventID
	owner.do(http.MethodPost, eventPath+"/ticket-types", ticketTypeInput("General", 10), http.StatusCreated, nil)
	hidden := ticketTypeInput("Comp", 5)
	hidden.Hidden = true
	owner.do(http.MethodPost, eventPath+"/ticket-types", hidden, http.StatusCreated, nil)

	publicPath := "/api/v1/events/" + eventID
	anonymous.do(http.MethodGet, publicPath, nil, http.StatusNotFound, nil)
	owner.do(http.MethodPost, eventPath+"/publish", nil, http.StatusOK, nil)
	var detail publicEventResponse
	anonymous.do(http.MethodGet, publicPath, nil, http.StatusOK, &detail)
	if detail.Event.ID != eventID || len(detail.TicketTypes) != 1 || detail.TicketTypes[0].Name != "General" {
		t.Fatalf("unexpected public detail: %+v", detail)
	}

	tests := []struct {
		visibility string
		wantStatus int
	}{
		{visibility: "unlisted", wantStatus: http.StatusOK},
		{visibility: "private", wantStatus: http.StatusNotFound},
	}
	for _, tt := range tests {
		owner.do(http.MethodPut, eventPath, eventInput(tt.visibility), http.StatusOK, nil)
		anonymous.do(http.MethodGet, publicPath, nil, tt.wantStatus, nil)
	}
	owner.do(http.MethodPost, eventPath+"/cancel", nil, http.StatusOK, nil)
	anonymous.do(http.MethodGet, publicPath, nil, http.StatusNotFound, nil)
	anonymous.do(http.MethodGet, "/api/v1/events/not-a-uuid", nil, http.StatusNotFound, nil)
}

func TestTicketTypeCapacityIsAtomic(t *testing.T) {
	e := newDBServer(t)
	owner := createOrganizerClient(t, e, "types-race")
	event := eventInput("public")
	event.Capacity = 10
	typesPath := "/api/v1/organizer/events/" + createEvent(t, owner, event) + "/ticket-types"
	body, err := json.Marshal(ticketTypeInput("Half plus one", 6))
	if err != nil {
		t.Fatal(err)
	}
	statuses := make(chan int, 2)
	var workers sync.WaitGroup
	for range 2 {
		workers.Add(1)
		go func() {
			defer workers.Done()
			req := httptest.NewRequest(http.MethodPost, typesPath, bytes.NewReader(body))
			req.Header.Set(echo.HeaderAuthorization, "Bearer "+owner.token)
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
		t.Fatalf("concurrent ticket type statuses = %v, want one 201 and one 409", counts)
	}
}
