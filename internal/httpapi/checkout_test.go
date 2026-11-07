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

const testTypeID = "00000000-0000-0000-0000-000000000011"

func TestProcessingFee(t *testing.T) {
	t.Parallel()
	tests := []struct {
		give int64
		want int64
	}{
		{give: 0, want: 0},
		{give: 10, want: 0},
		{give: 17, want: 1},
		{give: 5000, want: 150},
		{give: 10000, want: 300},
	}
	for _, tt := range tests {
		if got := store.ProcessingFee(tt.give, 3); got != tt.want {
			t.Fatalf("ProcessingFee(%d) = %d, want %d", tt.give, got, tt.want)
		}
	}
}

func checkoutInput(items ...store.CheckoutItem) store.CheckoutInput {
	return store.CheckoutInput{Items: items, AttendeeName: "Aigerim", AttendeeEmail: "aigerim@example.com"}
}

func TestValidateCheckout(t *testing.T) {
	t.Parallel()
	item := store.CheckoutItem{TicketTypeID: testTypeID, Quantity: 1}
	tooMany := make([]store.CheckoutItem, maxCheckoutItems+1)
	tests := []struct {
		name      string
		give      store.CheckoutInput
		wantError bool
	}{
		{name: "valid", give: checkoutInput(item)},
		{name: "no items", give: checkoutInput(), wantError: true},
		{name: "too many items", give: checkoutInput(tooMany...), wantError: true},
		{name: "duplicate type", give: checkoutInput(item, item), wantError: true},
		{name: "bad type", give: checkoutInput(store.CheckoutItem{TicketTypeID: "nope", Quantity: 1}), wantError: true},
		{name: "zero quantity", give: checkoutInput(store.CheckoutItem{TicketTypeID: testTypeID}), wantError: true},
		{name: "missing name", give: func() store.CheckoutInput { in := checkoutInput(item); in.AttendeeName = ""; return in }(), wantError: true},
		{name: "bad email", give: func() store.CheckoutInput { in := checkoutInput(item); in.AttendeeEmail = "nope"; return in }(), wantError: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			if got := validateCheckout(tt.give); (got != nil) != tt.wantError {
				t.Fatalf("validateCheckout() error = %v, wantError %v", got, tt.wantError)
			}
		})
	}
}

func TestCheckoutRoutesRequireAuthentication(t *testing.T) {
	t.Parallel()
	e := New(nil, nil, Options{NoAuthRateLimit: true})
	event := "/api/v1/organizer/events/00000000-0000-0000-0000-000000000010"
	order := "00000000-0000-0000-0000-000000000012"
	tests := []struct{ method, path string }{
		{http.MethodPost, "/api/v1/events/00000000-0000-0000-0000-000000000010/checkout"},
		{http.MethodGet, "/api/v1/me/orders"},
		{http.MethodGet, "/api/v1/me/orders/" + order},
		{http.MethodPost, "/api/v1/me/orders/" + order + "/pay"},
		{http.MethodPost, "/api/v1/me/orders/" + order + "/cancel"},
		{http.MethodGet, event + "/paid-sales"},
		{http.MethodPost, event + "/paid-sales/activate"},
		{http.MethodGet, event + "/sales"},
		{http.MethodGet, event + "/orders"},
		{http.MethodPost, event + "/orders/" + order + "/refund"},
		{http.MethodPost, event + "/orders/" + order + "/cancel"},
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

type orderResponse struct {
	Order   store.OrganizerOrder `json:"order"`
	Payment store.Payment        `json:"payment"`
	Tickets []store.Ticket       `json:"tickets"`
}

type paidSalesResponse struct {
	PaidSales store.PaidSales `json:"paidSales"`
}

func wantError(t *testing.T, c client, method, path string, body any, status int, code string) {
	t.Helper()
	var result Error
	c.do(method, path, body, status, &result)
	if result.Code != code {
		t.Fatalf("%s %s: code = %q, want %q", method, path, result.Code, code)
	}
}

func completeProfile(t *testing.T, owner client, name string) {
	t.Helper()
	profile := organizerInput(name)
	profile.PayoutAccountReference = "DEMO-PAYOUT-1"
	owner.do(http.MethodPut, "/api/v1/organizer/profile", profile, http.StatusOK, nil)
}

func activate(owner client, eventID string) {
	owner.do(http.MethodPost, "/api/v1/organizer/events/"+eventID+"/paid-sales/activate",
		activationInput{AcceptTerms: true}, http.StatusOK, nil)
}

func paidEvent(t *testing.T, e *echo.Echo, name string, price int64, quantity, maxPerOrder int) (client, string, string) {
	t.Helper()
	owner := createOrganizerClient(t, e, name)
	completeProfile(t, owner, name)
	eventID := createEvent(t, owner, eventInput("public"))
	in := ticketTypeInput("Standard", quantity)
	in.PriceAmount, in.MaxPerOrder = price, maxPerOrder
	typeID := createTicketType(t, owner, eventID, in)
	activate(owner, eventID)
	owner.do(http.MethodPost, "/api/v1/organizer/events/"+eventID+"/publish", nil, http.StatusOK, nil)
	return owner, eventID, typeID
}

func TestPaidSalesActivation(t *testing.T) {
	e := newDBServer(t)
	owner := createOrganizerClient(t, e, "activation")
	eventID := createEvent(t, owner, eventInput("public"))
	base := "/api/v1/organizer/events/" + eventID + "/paid-sales"

	var got paidSalesResponse
	owner.do(http.MethodGet, base, nil, http.StatusOK, &got)
	if got.PaidSales.Status != "inactive" || !got.PaidSales.Demo || got.PaidSales.FeeAmount != store.PaidSalesActivationFee {
		t.Fatalf("unexpected paid sales: %+v", got.PaidSales)
	}
	if got.PaidSales.Checklist.PaidTicketType || got.PaidSales.Checklist.PayoutAccount || !got.PaidSales.Checklist.ContactPhone {
		t.Fatalf("unexpected checklist: %+v", got.PaidSales.Checklist)
	}
	wantError(t, owner, http.MethodPost, base+"/activate", activationInput{}, http.StatusBadRequest, "validation_failed")
	wantError(t, owner, http.MethodPost, base+"/activate", activationInput{AcceptTerms: true}, http.StatusConflict, "activation_incomplete")

	paid := ticketTypeInput("Paid", 10)
	paid.PriceAmount = 5000
	createTicketType(t, owner, eventID, paid)
	completeProfile(t, owner, "activation")
	owner.do(http.MethodPost, base+"/activate", activationInput{AcceptTerms: true}, http.StatusOK, &got)
	if got.PaidSales.Status != "active" || got.PaidSales.ActivatedAt == nil {
		t.Fatalf("activation did not apply: %+v", got.PaidSales)
	}
	owner.do(http.MethodPost, base+"/activate", activationInput{AcceptTerms: true}, http.StatusOK, nil)

	other := createOrganizerClient(t, e, "activation-other")
	other.do(http.MethodGet, base, nil, http.StatusNotFound, nil)
	other.do(http.MethodPost, base+"/activate", activationInput{AcceptTerms: true}, http.StatusNotFound, nil)
}

func TestPaidCheckoutPaymentAndRefund(t *testing.T) {
	e := newDBServer(t)
	owner, eventID, typeID := paidEvent(t, e, "paid-flow", 5000, 10, 4)
	attendee := signInWithCode(t, e, uniqueEmail("buyer"))
	checkoutPath := "/api/v1/events/" + eventID + "/checkout"
	item := store.CheckoutItem{TicketTypeID: typeID, Quantity: 2}

	var created orderResponse
	attendee.do(http.MethodPost, checkoutPath, checkoutInput(item), http.StatusCreated, &created)
	o := created.Order
	if o.Status != "pending" || o.TotalAmount != 10000 || o.ExpiresAt == nil || len(o.Items) != 1 || o.Items[0].UnitPriceAmount != 5000 {
		t.Fatalf("unexpected pending order: %+v", o)
	}
	attendee.do(http.MethodPost, "/api/v1/events/"+eventID+"/register", registration(typeID, 1), http.StatusConflict, nil)

	orderPath := "/api/v1/me/orders/" + o.ID
	var paid orderResponse
	attendee.do(http.MethodPost, orderPath+"/pay", payInput{Outcome: "failed"}, http.StatusOK, &paid)
	if paid.Payment.Status != "failed" || !paid.Payment.Demo || paid.Order.Status != "pending" {
		t.Fatalf("failed payment changed the order: %+v", paid)
	}
	attendee.do(http.MethodGet, orderPath, nil, http.StatusOK, &paid)
	if len(paid.Tickets) != 0 {
		t.Fatalf("failed payment issued tickets: %+v", paid.Tickets)
	}
	attendee.do(http.MethodPost, orderPath+"/pay", payInput{Outcome: "succeeded"}, http.StatusOK, &paid)
	if paid.Payment.Status != "succeeded" || paid.Order.Status != "paid" || len(paid.Order.Payments) != 2 {
		t.Fatalf("unexpected paid order: %+v", paid)
	}
	wantError(t, attendee, http.MethodPost, orderPath+"/pay", payInput{Outcome: "succeeded"}, http.StatusConflict, "invalid_order_state")
	attendee.do(http.MethodGet, orderPath, nil, http.StatusOK, &paid)
	if len(paid.Tickets) != 2 || paid.Tickets[0].Status != "valid" {
		t.Fatalf("unexpected tickets: %+v", paid.Tickets)
	}

	stranger := signInWithCode(t, e, uniqueEmail("stranger"))
	stranger.do(http.MethodGet, orderPath, nil, http.StatusNotFound, nil)
	stranger.do(http.MethodPost, orderPath+"/pay", payInput{Outcome: "succeeded"}, http.StatusNotFound, nil)

	assertSales(t, owner, eventID, store.SalesSummary{GrossAmount: 10000, ProcessingFeeAmount: 300,
		EstimatedPayoutAmount: 9700, PaidOrders: 1, TicketsSold: 2, Demo: true})

	eventOrders := "/api/v1/organizer/events/" + eventID + "/orders/"
	wantError(t, owner, http.MethodPost, eventOrders+o.ID+"/cancel", nil, http.StatusConflict, "refund_required")
	var refunded orderResponse
	owner.do(http.MethodPost, eventOrders+o.ID+"/refund", refundInput{Reason: "Requested"}, http.StatusOK, &refunded)
	if refunded.Order.Status != "refunded" || refunded.Order.BuyerEmail == "" {
		t.Fatalf("unexpected refunded order: %+v", refunded.Order)
	}
	wantError(t, owner, http.MethodPost, eventOrders+o.ID+"/refund", refundInput{}, http.StatusConflict, "invalid_order_state")
	attendee.do(http.MethodGet, orderPath, nil, http.StatusOK, &paid)
	if paid.Tickets[0].Status != "refunded" || paid.Tickets[1].Status != "refunded" {
		t.Fatalf("refund left tickets valid: %+v", paid.Tickets)
	}
	assertSales(t, owner, eventID, store.SalesSummary{GrossAmount: 10000, RefundedAmount: 10000, Demo: true})

	createOrganizerClient(t, e, "paid-flow-other").do(http.MethodPost, eventOrders+o.ID+"/refund", refundInput{}, http.StatusNotFound, nil)
}

func assertSales(t *testing.T, owner client, eventID string, want store.SalesSummary) {
	t.Helper()
	var got struct {
		Sales store.SalesSummary `json:"sales"`
	}
	owner.do(http.MethodGet, "/api/v1/organizer/events/"+eventID+"/sales", nil, http.StatusOK, &got)
	if got.Sales != want {
		t.Fatalf("sales = %+v, want %+v", got.Sales, want)
	}
}

func TestCheckoutRules(t *testing.T) {
	e := newDBServer(t)
	owner := createOrganizerClient(t, e, "checkout-rules")
	eventID := createEvent(t, owner, eventInput("public"))
	paid := ticketTypeInput("Paid", 5)
	paid.PriceAmount = 3000
	paidID := createTicketType(t, owner, eventID, paid)
	freeID := createTicketType(t, owner, eventID, ticketTypeInput("Free", 5))
	owner.do(http.MethodPost, "/api/v1/organizer/events/"+eventID+"/publish", nil, http.StatusOK, nil)
	attendee := signInWithCode(t, e, uniqueEmail("checkout-rules-buyer"))
	path := "/api/v1/events/" + eventID + "/checkout"

	wantError(t, attendee, http.MethodPost, path, checkoutInput(store.CheckoutItem{TicketTypeID: paidID, Quantity: 1}),
		http.StatusConflict, "paid_sales_inactive")

	var free orderResponse
	attendee.do(http.MethodPost, path, checkoutInput(store.CheckoutItem{TicketTypeID: freeID, Quantity: 2}), http.StatusCreated, &free)
	if free.Order.Status != "paid" || free.Order.TotalAmount != 0 || len(free.Order.Payments) != 0 {
		t.Fatalf("free checkout should complete without payment: %+v", free.Order)
	}

	eventOrders := "/api/v1/organizer/events/" + eventID + "/orders"
	var list struct {
		Orders []store.OrganizerOrder `json:"orders"`
	}
	owner.do(http.MethodGet, eventOrders, nil, http.StatusOK, &list)
	if len(list.Orders) != 1 || list.Orders[0].AttendeeName != "Aigerim" {
		t.Fatalf("unexpected organizer orders: %+v", list.Orders)
	}
	wantError(t, owner, http.MethodPost, eventOrders+"/"+free.Order.ID+"/refund", refundInput{}, http.StatusConflict, "free_order")
	var cancelled orderResponse
	owner.do(http.MethodPost, eventOrders+"/"+free.Order.ID+"/cancel", nil, http.StatusOK, &cancelled)
	if cancelled.Order.Status != "cancelled" {
		t.Fatalf("unexpected cancelled order: %+v", cancelled.Order)
	}
	var mine struct {
		Tickets []store.Ticket `json:"tickets"`
	}
	attendee.do(http.MethodGet, "/api/v1/me/tickets", nil, http.StatusOK, &mine)
	if len(mine.Tickets) != 2 || mine.Tickets[0].Status != "cancelled" {
		t.Fatalf("cancelled registration left tickets valid: %+v", mine.Tickets)
	}
	attendee.do(http.MethodPost, "/api/v1/events/"+eventID+"/register", registration(freeID, 2), http.StatusCreated, nil)
}

func TestCheckoutHoldsInventory(t *testing.T) {
	e := newDBServer(t)
	_, eventID, typeID := paidEvent(t, e, "holds", 2000, 2, 2)
	path := "/api/v1/events/" + eventID + "/checkout"
	first := signInWithCode(t, e, uniqueEmail("holder"))
	second := signInWithCode(t, e, uniqueEmail("waiter"))

	var held orderResponse
	first.do(http.MethodPost, path, checkoutInput(store.CheckoutItem{TicketTypeID: typeID, Quantity: 2}), http.StatusCreated, &held)
	wantError(t, second, http.MethodPost, path, checkoutInput(store.CheckoutItem{TicketTypeID: typeID, Quantity: 1}), http.StatusConflict, "sold_out")

	var replaced orderResponse
	first.do(http.MethodPost, path, checkoutInput(store.CheckoutItem{TicketTypeID: typeID, Quantity: 1}), http.StatusCreated, &replaced)
	var old orderResponse
	first.do(http.MethodGet, "/api/v1/me/orders/"+held.Order.ID, nil, http.StatusOK, &old)
	if old.Order.Status != "cancelled" {
		t.Fatalf("a new checkout must release the previous hold: %+v", old.Order)
	}
	second.do(http.MethodPost, path, checkoutInput(store.CheckoutItem{TicketTypeID: typeID, Quantity: 1}), http.StatusCreated, nil)

	first.do(http.MethodPost, "/api/v1/me/orders/"+replaced.Order.ID+"/cancel", nil, http.StatusOK, nil)
	wantError(t, first, http.MethodPost, "/api/v1/me/orders/"+replaced.Order.ID+"/pay", payInput{Outcome: "succeeded"}, http.StatusConflict, "invalid_order_state")
	wantError(t, first, http.MethodPost, "/api/v1/me/orders/"+replaced.Order.ID+"/cancel", nil, http.StatusConflict, "invalid_order_state")

	var orders struct {
		Orders []store.Order `json:"orders"`
	}
	first.do(http.MethodGet, "/api/v1/me/orders", nil, http.StatusOK, &orders)
	if len(orders.Orders) != 2 {
		t.Fatalf("unexpected my orders: %+v", orders.Orders)
	}
}

func TestCheckoutDoesNotOversell(t *testing.T) {
	e := newDBServer(t)
	_, eventID, typeID := paidEvent(t, e, "checkout-race", 1000, 2, 1)
	body, err := json.Marshal(checkoutInput(store.CheckoutItem{TicketTypeID: typeID, Quantity: 1}))
	if err != nil {
		t.Fatal(err)
	}
	const buyers = 5
	tokens := make([]string, buyers)
	for i := range tokens {
		tokens[i] = signInWithCode(t, e, uniqueEmail("checkout-racer")).token
	}
	statuses := make(chan int, buyers)
	var workers sync.WaitGroup
	for _, token := range tokens {
		workers.Add(1)
		go func() {
			defer workers.Done()
			req := httptest.NewRequest(http.MethodPost, "/api/v1/events/"+eventID+"/checkout", bytes.NewReader(body))
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
	if counts[http.StatusCreated] != 2 || counts[http.StatusConflict] != buyers-2 {
		t.Fatalf("concurrent checkout statuses = %v, want two 201 and three 409", counts)
	}
}

func TestPaymentIsChargedOnce(t *testing.T) {
	e := newDBServer(t)
	_, eventID, typeID := paidEvent(t, e, "pay-race", 1000, 5, 5)
	buyer := signInWithCode(t, e, uniqueEmail("pay-racer"))
	var created orderResponse
	buyer.do(http.MethodPost, "/api/v1/events/"+eventID+"/checkout",
		checkoutInput(store.CheckoutItem{TicketTypeID: typeID, Quantity: 1}), http.StatusCreated, &created)
	body, err := json.Marshal(payInput{Outcome: "succeeded"})
	if err != nil {
		t.Fatal(err)
	}
	const attempts = 4
	statuses := make(chan int, attempts)
	var workers sync.WaitGroup
	for range attempts {
		workers.Add(1)
		go func() {
			defer workers.Done()
			req := httptest.NewRequest(http.MethodPost, "/api/v1/me/orders/"+created.Order.ID+"/pay", bytes.NewReader(body))
			req.Header.Set(echo.HeaderAuthorization, "Bearer "+buyer.token)
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
	var got orderResponse
	buyer.do(http.MethodGet, "/api/v1/me/orders/"+created.Order.ID, nil, http.StatusOK, &got)
	if counts[http.StatusOK] != 1 || len(got.Order.Payments) != 1 || len(got.Tickets) != 1 {
		t.Fatalf("statuses = %v, payments %d, tickets %d; want one charge", counts, len(got.Order.Payments), len(got.Tickets))
	}
}
