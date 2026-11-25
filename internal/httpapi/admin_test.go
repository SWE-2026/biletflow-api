package httpapi

import (
	"context"
	"encoding/csv"
	"net/http"
	"net/http/httptest"
	"os"
	"slices"
	"strings"
	"testing"

	"github.com/SWE-2026/biletflow-api/internal/store"
	"github.com/labstack/echo/v4"
)

// signInAsAdmin creates an account and promotes it the way ADMIN_EMAILS does on start.
func signInAsAdmin(t *testing.T, e *echo.Echo) client {
	t.Helper()
	email := uniqueEmail("admin")
	admin := signInWithCode(t, e, email)
	ctx := context.Background()
	db, err := store.NewPostgres(ctx, os.Getenv("TEST_DB_DSN"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if _, err := db.PromoteAdmins(ctx, []string{email}); err != nil {
		t.Fatal(err)
	}
	return admin
}

// findActivation lists activations with the given status and returns the event's, if listed.
func findActivation(c client, status, eventID string) (store.AdminActivation, bool) {
	c.t.Helper()
	var got struct {
		PaidSales []store.AdminActivation `json:"paidSales"`
	}
	c.do(http.MethodGet, "/api/v1/admin/paid-sales?status="+status, nil, http.StatusOK, &got)
	for _, a := range got.PaidSales {
		if a.EventID == eventID {
			return a, true
		}
	}
	return store.AdminActivation{}, false
}

func TestAdminReviewsPaidSales(t *testing.T) {
	e := newDBServer(t)
	owner := createOrganizerClient(t, e, "review-owner")
	completeProfile(t, owner, "review-owner")
	eventID := createEvent(t, owner, eventInput("public"))
	paid := ticketTypeInput("Standard", 10)
	paid.PriceAmount = 5000
	createTicketType(t, owner, eventID, paid)
	owner.do(http.MethodPost, "/api/v1/organizer/events/"+eventID+"/paid-sales/activate",
		activationInput{AcceptTerms: true}, http.StatusOK, nil)

	owner.do(http.MethodGet, "/api/v1/admin/paid-sales", nil, http.StatusForbidden, nil)
	admin := signInAsAdmin(t, e)
	got, ok := findActivation(admin, "pending", eventID)
	if !ok {
		t.Fatal("pending request not listed")
	}
	if got.RequestedAt == nil || got.PayoutAccountReference != "DEMO-PAYOUT-1" || got.ContactPhone == "" || !got.Demo {
		t.Fatalf("activation is missing review details: %+v", got)
	}

	review := "/api/v1/admin/events/" + eventID + "/paid-sales/"
	admin.do(http.MethodPost, review+"reject", map[string]string{}, http.StatusBadRequest, nil)
	admin.do(http.MethodPost, review+"reject", map[string]string{"note": "Payout label does not match."}, http.StatusNoContent, nil)
	if got, _ = findActivation(admin, "rejected", eventID); got.ReviewNote != "Payout label does not match." {
		t.Fatalf("reviewNote = %q", got.ReviewNote)
	}
	wantError(t, admin, http.MethodPost, review+"approve", map[string]string{}, http.StatusConflict, "invalid_state")

	owner.do(http.MethodPost, "/api/v1/organizer/events/"+eventID+"/paid-sales/activate",
		activationInput{AcceptTerms: true}, http.StatusOK, nil)
	admin.do(http.MethodPost, review+"approve", map[string]string{}, http.StatusNoContent, nil)
	if got, ok = findActivation(admin, "active", eventID); !ok || got.ActivatedAt == nil {
		t.Fatalf("approved activation: %+v, listed %v", got, ok)
	}
}

func TestAdminFindsOrdersByShortNumberAndExportsDemoFlag(t *testing.T) {
	e := newDBServer(t)
	owner, eventID, typeID := paidEvent(t, e, "admin-search", 5000, 10, 4)
	buyer := signInWithCode(t, e, uniqueEmail("admin-search-buyer"))
	var created orderResponse
	buyer.do(http.MethodPost, "/api/v1/events/"+eventID+"/checkout",
		checkoutInput(store.CheckoutItem{TicketTypeID: typeID, Quantity: 1}), http.StatusCreated, &created)
	orderID := created.Order.ID
	buyer.do(http.MethodPost, "/api/v1/me/orders/"+orderID+"/pay", payInput{Outcome: "succeeded"}, http.StatusOK, nil)

	admin := signInAsAdmin(t, e)
	short := strings.ToUpper(orderID[:8])
	tests := []struct {
		give string
		want int
	}{
		{give: orderID, want: 1},
		{give: short, want: 1},
		{give: orderID[:7], want: 0},
	}
	for _, tt := range tests {
		var orders struct {
			Orders []store.OrganizerOrder `json:"orders"`
		}
		admin.do(http.MethodGet, "/api/v1/admin/orders?q="+tt.give, nil, http.StatusOK, &orders)
		var payments struct {
			Payments []store.AdminPayment `json:"payments"`
		}
		admin.do(http.MethodGet, "/api/v1/admin/payments?q="+tt.give, nil, http.StatusOK, &payments)
		if len(orders.Orders) != tt.want || len(payments.Payments) != tt.want {
			t.Errorf("q=%s: %d orders, %d payments, want %d", tt.give, len(orders.Orders), len(payments.Payments), tt.want)
		}
	}

	owner.do(http.MethodPost, "/api/v1/organizer/events/"+eventID+"/orders/"+orderID+"/refund", refundInput{}, http.StatusOK, nil)
	var activity struct {
		Activity []store.ActivityEntry `json:"activity"`
	}
	owner.do(http.MethodGet, "/api/v1/organizer/events/"+eventID+"/activity?type=refund", nil, http.StatusOK, &activity)
	if len(activity.Activity) != 1 || !strings.HasSuffix(activity.Activity[0].Description, "for order "+short) {
		t.Fatalf("refund entry does not name its order: %+v", activity.Activity)
	}

	req := httptest.NewRequest(http.MethodGet, "/api/v1/admin/export/orders", nil)
	req.Header.Set(echo.HeaderAuthorization, "Bearer "+admin.token)
	rec := httptest.NewRecorder()
	e.ServeHTTP(rec, req)
	records, err := csv.NewReader(rec.Body).ReadAll()
	if err != nil || rec.Code != http.StatusOK {
		t.Fatalf("export: status %d, err %v", rec.Code, err)
	}
	demo := slices.Index(records[0], "demo")
	for _, r := range records[1:] {
		if r[0] == orderID {
			if demo < 0 || r[demo] != "true" {
				t.Fatalf("order row has no demo flag: header %v, row %v", records[0], r)
			}
			return
		}
	}
	t.Fatal("order missing from the export")
}
