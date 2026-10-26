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

func TestSaleWindowOpen(t *testing.T) {
	t.Parallel()
	now := time.Now().UTC()
	past, future := now.Add(-time.Hour), now.Add(time.Hour)
	starts := now.Add(48 * time.Hour)
	tests := []struct {
		name string
		give store.SaleWindow
		want bool
	}{
		{name: "no limits", give: store.SaleWindow{EventStartsAt: starts}, want: true},
		{name: "inside both windows", give: store.SaleWindow{SalesStartsAt: &past, SalesEndsAt: &future, RegistrationOpensAt: &past, RegistrationClosesAt: &future, EventStartsAt: starts}, want: true},
		{name: "sales not started", give: store.SaleWindow{SalesStartsAt: &future, EventStartsAt: starts}},
		{name: "sales ended", give: store.SaleWindow{SalesEndsAt: &past, EventStartsAt: starts}},
		{name: "registration not open", give: store.SaleWindow{RegistrationOpensAt: &future, EventStartsAt: starts}},
		{name: "registration closed", give: store.SaleWindow{RegistrationClosesAt: &past, EventStartsAt: starts}},
		{name: "event started", give: store.SaleWindow{EventStartsAt: past}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			if got := tt.give.Open(now); got != tt.want {
				t.Fatalf("Open() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestValidateRegistration(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name      string
		give      registrationInput
		wantError bool
	}{
		{name: "valid", give: registrationInput{TicketTypeID: "00000000-0000-0000-0000-000000000011", Quantity: 1}},
		{name: "bad ticket type", give: registrationInput{TicketTypeID: "nope", Quantity: 1}, wantError: true},
		{name: "zero quantity", give: registrationInput{TicketTypeID: "00000000-0000-0000-0000-000000000011"}, wantError: true},
		{name: "too many", give: registrationInput{TicketTypeID: "00000000-0000-0000-0000-000000000011", Quantity: 101}, wantError: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			if got := validateRegistration(tt.give); (got != nil) != tt.wantError {
				t.Fatalf("validateRegistration() error = %v, wantError %v", got, tt.wantError)
			}
		})
	}
}

func TestRegistrationRoutesRequireAuthentication(t *testing.T) {
	t.Parallel()
	e := New(nil, nil, Options{NoAuthRateLimit: true})
	tests := []struct{ method, path string }{
		{http.MethodPost, "/api/v1/events/00000000-0000-0000-0000-000000000010/register"},
		{http.MethodGet, "/api/v1/me/tickets"},
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

type registrationResponse struct {
	OrderID string         `json:"orderId"`
	Tickets []store.Ticket `json:"tickets"`
}

// createTicketType adds a ticket type to the organizer's event and returns its ID.
func createTicketType(t *testing.T, c client, eventID string, in store.TicketTypeInput) string {
	t.Helper()
	var created ticketTypeResponse
	c.do(http.MethodPost, "/api/v1/organizer/events/"+eventID+"/ticket-types", in, http.StatusCreated, &created)
	return created.TicketType.ID
}

func registration(ticketTypeID string, quantity int) registrationInput {
	return registrationInput{TicketTypeID: ticketTypeID, Quantity: quantity}
}

// wantCode registers and checks the error code.
func wantCode(t *testing.T, c client, path string, in registrationInput, status int, code string) {
	t.Helper()
	var result Error
	c.do(http.MethodPost, path, in, status, &result)
	if result.Code != code {
		t.Fatalf("code = %q, want %q", result.Code, code)
	}
}

func TestFreeRegistration(t *testing.T) {
	e := newDBServer(t)
	owner := createOrganizerClient(t, e, "reg-owner")
	eventID := createEvent(t, owner, eventInput("public"))
	free := ticketTypeInput("Free", 3)
	free.MaxPerOrder = 2
	freeID := createTicketType(t, owner, eventID, free)
	paid := ticketTypeInput("Paid", 5)
	paid.PriceAmount = 5000
	paidID := createTicketType(t, owner, eventID, paid)
	hidden := ticketTypeInput("Hidden", 5)
	hidden.Hidden = true
	hiddenID := createTicketType(t, owner, eventID, hidden)
	registerPath := "/api/v1/events/" + eventID + "/register"

	first := signInWithCode(t, e, uniqueEmail("attendee-one"))
	first.do(http.MethodPost, registerPath, registration(freeID, 1), http.StatusNotFound, nil)
	owner.do(http.MethodPost, "/api/v1/organizer/events/"+eventID+"/publish", nil, http.StatusOK, nil)

	var got registrationResponse
	first.do(http.MethodPost, registerPath, registration(freeID, 2), http.StatusCreated, &got)
	if got.OrderID == "" || len(got.Tickets) != 2 || got.Tickets[0].QRToken == got.Tickets[1].QRToken {
		t.Fatalf("unexpected registration: %+v", got)
	}
	if got.Tickets[0].Status != "valid" || got.Tickets[0].Event.ID != eventID || got.Tickets[0].TicketTypeName != "Free" {
		t.Fatalf("unexpected ticket: %+v", got.Tickets[0])
	}
	wantCode(t, first, registerPath, registration(freeID, 1), http.StatusConflict, "ticket_limit_reached")
	wantCode(t, first, registerPath, registration(paidID, 1), http.StatusConflict, "payment_required")
	first.do(http.MethodPost, registerPath, registration(hiddenID, 1), http.StatusNotFound, nil)

	second := signInWithCode(t, e, uniqueEmail("attendee-two"))
	wantCode(t, second, registerPath, registration(freeID, 2), http.StatusConflict, "sold_out")
	second.do(http.MethodPost, registerPath, registration(freeID, 1), http.StatusCreated, nil)

	var mine struct {
		Tickets []store.Ticket `json:"tickets"`
	}
	first.do(http.MethodGet, "/api/v1/me/tickets", nil, http.StatusOK, &mine)
	if len(mine.Tickets) != 2 || mine.Tickets[0].Event.Title != "BiletFlow Demo" {
		t.Fatalf("unexpected my tickets: %+v", mine.Tickets)
	}

	typePath := "/api/v1/organizer/events/" + eventID + "/ticket-types/" + freeID
	var result Error
	owner.do(http.MethodPut, typePath, ticketTypeInput("Free", 2), http.StatusConflict, &result)
	if result.Code != "quantity_below_sold" {
		t.Fatalf("code = %q, want quantity_below_sold", result.Code)
	}
	owner.do(http.MethodPut, typePath, ticketTypeInput("Free", 3), http.StatusOK, nil)
	owner.do(http.MethodDelete, typePath, nil, http.StatusConflict, nil)
}

func TestRegistrationAccessRules(t *testing.T) {
	e := newDBServer(t)
	owner := createOrganizerClient(t, e, "reg-rules")
	attendee := signInWithCode(t, e, uniqueEmail("reg-rules-attendee"))

	closed := eventInput("public")
	opens, closes := time.Now().UTC().Add(-2*time.Hour), time.Now().UTC().Add(-time.Hour)
	closed.RegistrationOpensAt, closed.RegistrationClosesAt = &opens, &closes
	closedID := createEvent(t, owner, closed)
	closedType := createTicketType(t, owner, closedID, ticketTypeInput("Free", 5))
	owner.do(http.MethodPost, "/api/v1/organizer/events/"+closedID+"/publish", nil, http.StatusOK, nil)
	wantCode(t, attendee, "/api/v1/events/"+closedID+"/register", registration(closedType, 1), http.StatusConflict, "registration_closed")

	privateID := createEvent(t, owner, eventInput("private"))
	privateType := createTicketType(t, owner, privateID, ticketTypeInput("Free", 5))
	owner.do(http.MethodPost, "/api/v1/organizer/events/"+privateID+"/publish", nil, http.StatusOK, nil)
	attendee.do(http.MethodPost, "/api/v1/events/"+privateID+"/register", registration(privateType, 1), http.StatusNotFound, nil)

	// A ticket type only registers under its own event.
	attendee.do(http.MethodPost, "/api/v1/events/"+closedID+"/register", registration(privateType, 1), http.StatusNotFound, nil)

	var signup sessionResponse
	client{t: t, e: e}.do(http.MethodPost, "/api/v1/auth/signup", map[string]string{
		"email": uniqueEmail("reg-unverified"), "password": "correct horse", "displayName": "Unverified",
	}, http.StatusCreated, &signup)
	unverified := client{t: t, e: e, token: signup.Token}
	wantCode(t, unverified, "/api/v1/events/"+closedID+"/register", registration(closedType, 1), http.StatusForbidden, "email_unverified")
}

func TestRegistrationDoesNotOversell(t *testing.T) {
	e := newDBServer(t)
	owner := createOrganizerClient(t, e, "reg-race")
	eventID := createEvent(t, owner, eventInput("public"))
	limited := ticketTypeInput("Limited", 2)
	limited.MaxPerOrder = 1
	typeID := createTicketType(t, owner, eventID, limited)
	owner.do(http.MethodPost, "/api/v1/organizer/events/"+eventID+"/publish", nil, http.StatusOK, nil)
	body, err := json.Marshal(registration(typeID, 1))
	if err != nil {
		t.Fatal(err)
	}

	const attendees = 5
	tokens := make([]string, attendees)
	for i := range tokens {
		tokens[i] = signInWithCode(t, e, uniqueEmail("racer")).token
	}
	statuses := make(chan int, attendees)
	var workers sync.WaitGroup
	for _, token := range tokens {
		workers.Add(1)
		go func() {
			defer workers.Done()
			req := httptest.NewRequest(http.MethodPost, "/api/v1/events/"+eventID+"/register", bytes.NewReader(body))
			req.Header.Set(echo.HeaderAuthorization, "Bearer "+token)
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
	if counts[http.StatusCreated] != 2 || counts[http.StatusConflict] != attendees-2 {
		t.Fatalf("concurrent registration statuses = %v, want two 201 and three 409", counts)
	}
}
