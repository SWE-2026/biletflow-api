package httpapi

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"

	"github.com/SWE-2026/biletflow-api/internal/store"
	"github.com/labstack/echo/v4"
)

type seatingResponse struct {
	Seating store.EventSeating `json:"seating"`
}

func seatedEvent(t *testing.T, e *echo.Echo, name string) (client, string) {
	t.Helper()
	owner := createOrganizerClient(t, e, name)
	in := eventInput("public")
	in.Capacity = 200
	eventID := createEvent(t, owner, in)
	categories := map[string]string{}
	for _, category := range []string{"vip", "standard", "economy"} {
		categories[category] = createTicketType(t, owner, eventID, ticketTypeInput(category, 1))
	}
	path := "/api/v1/organizer/events/" + eventID
	owner.do(http.MethodPut, path+"/seating", store.SeatingInput{Mode: "assigned", LayoutKey: "nope", Categories: categories},
		http.StatusBadRequest, nil)
	var configured struct {
		Seating store.OrganizerSeating `json:"seating"`
	}
	owner.do(http.MethodPut, path+"/seating", store.SeatingInput{Mode: "assigned", LayoutKey: "hall_a", Categories: categories},
		http.StatusOK, &configured)
	if configured.Seating.Mode != "assigned" || len(configured.Seating.Categories) != 3 || configured.Seating.Locked {
		t.Fatalf("unexpected seating: %+v", configured.Seating)
	}
	owner.do(http.MethodPost, path+"/publish", nil, http.StatusOK, nil)
	return owner, eventID
}

func availableSeats(t *testing.T, e *echo.Echo, eventID string) []store.Seat {
	t.Helper()
	var result seatingResponse
	client{t: t, e: e}.do(http.MethodGet, "/api/v1/events/"+eventID+"/seating", nil, http.StatusOK, &result)
	var seats []store.Seat
	for _, s := range result.Seating.Seats {
		if s.Status == "available" {
			seats = append(seats, s)
		}
	}
	return seats
}

func seatCheckout(seatIDs ...string) store.CheckoutInput {
	in := checkoutInput()
	in.SeatIDs = seatIDs
	return in
}

func TestAssignedSeating(t *testing.T) {
	e := newDBServer(t)
	owner, eventID := seatedEvent(t, e, "seating")
	var initial seatingResponse
	client{t: t, e: e}.do(http.MethodGet, "/api/v1/events/"+eventID+"/seating", nil, http.StatusOK, &initial)
	accessible := 0
	for _, s := range initial.Seating.Seats {
		if s.Accessible {
			accessible++
		}
	}
	if initial.Seating.Layout == nil || len(initial.Seating.Seats) != 138 || accessible != 4 {
		t.Fatalf("seat map: layout %v, %d seats, %d accessible", initial.Seating.Layout, len(initial.Seating.Seats), accessible)
	}

	seats := availableSeats(t, e, eventID)
	buyer := signInWithCode(t, e, uniqueEmail("seat-buyer"))
	checkoutPath := "/api/v1/events/" + eventID + "/checkout"
	wantError(t, buyer, http.MethodPost, checkoutPath, checkoutInput(store.CheckoutItem{TicketTypeID: *seats[0].TicketTypeID, Quantity: 1}),
		http.StatusConflict, "seat_selection_required")
	wantCode(t, buyer, "/api/v1/events/"+eventID+"/register", registration(*seats[0].TicketTypeID, 1), http.StatusConflict, "seat_selection_required")

	var created orderResponse
	buyer.do(http.MethodPost, checkoutPath, seatCheckout(seats[0].ID, seats[1].ID), http.StatusCreated, &created)
	if created.Order.Status != "paid" || len(created.Order.Items) != 1 || len(created.Order.Items[0].Seats) != 2 {
		t.Fatalf("seated order: %+v", created.Order)
	}
	var detail orderResponse
	buyer.do(http.MethodGet, "/api/v1/me/orders/"+created.Order.ID, nil, http.StatusOK, &detail)
	if len(detail.Tickets) != 2 || detail.Tickets[0].Seat == nil || detail.Tickets[0].Seat.Row != seats[0].Row {
		t.Fatalf("tickets must carry their seats: %+v", detail.Tickets)
	}

	other := signInWithCode(t, e, uniqueEmail("seat-other"))
	wantError(t, other, http.MethodPost, checkoutPath, seatCheckout(seats[1].ID), http.StatusConflict, "seat_unavailable")
	if got := len(availableSeats(t, e, eventID)); got != len(seats)-2 {
		t.Fatalf("available seats = %d, want %d", got, len(seats)-2)
	}
	wantError(t, owner, http.MethodPut, "/api/v1/organizer/events/"+eventID+"/seating",
		store.SeatingInput{Mode: "general"}, http.StatusConflict, "seating_locked")

	general := createOrganizerClient(t, e, "general")
	generalID := createEvent(t, general, eventInput("public"))
	createTicketType(t, general, generalID, ticketTypeInput("General", 5))
	general.do(http.MethodPost, "/api/v1/organizer/events/"+generalID+"/publish", nil, http.StatusOK, nil)
	wantError(t, other, http.MethodPost, "/api/v1/events/"+generalID+"/checkout", seatCheckout(seats[2].ID),
		http.StatusConflict, "seating_not_assigned")
}

func TestSeatIsSoldOnce(t *testing.T) {
	e := newDBServer(t)
	_, eventID := seatedEvent(t, e, "seat-race")
	seat := availableSeats(t, e, eventID)[0]
	body, err := json.Marshal(seatCheckout(seat.ID))
	if err != nil {
		t.Fatal(err)
	}
	const buyers = 5
	tokens := make([]string, buyers)
	for i := range tokens {
		tokens[i] = signInWithCode(t, e, uniqueEmail("seat-racer")).token
	}
	statuses := make(chan int, buyers)
	var workers sync.WaitGroup
	for _, token := range tokens {
		workers.Go(func() {
			req := httptest.NewRequest(http.MethodPost, "/api/v1/events/"+eventID+"/checkout", bytes.NewReader(body))
			req.Header.Set(echo.HeaderAuthorization, "Bearer "+token)
			req.Header.Set(echo.HeaderContentType, echo.MIMEApplicationJSON)
			rec := httptest.NewRecorder()
			e.ServeHTTP(rec, req)
			statuses <- rec.Code
		})
	}
	workers.Wait()
	close(statuses)
	counts := map[int]int{}
	for status := range statuses {
		counts[status]++
	}
	if counts[http.StatusCreated] != 1 || counts[http.StatusConflict] != buyers-1 {
		t.Fatalf("concurrent seat checkout statuses = %v, want one 201 and four 409", counts)
	}
}
