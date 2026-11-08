package httpapi

import (
	"net/http"
	"slices"
	"testing"

	"github.com/SWE-2026/biletflow-api/internal/store"
)

// notificationLinks lists the links of the client's notifications by kind.
func notificationLinks(c client) map[string][]string {
	c.t.Helper()
	var got struct {
		Notifications []store.Notification `json:"notifications"`
	}
	c.do(http.MethodGet, "/api/v1/me/notifications", nil, http.StatusOK, &got)
	links := map[string][]string{}
	for _, n := range got.Notifications {
		links[n.Kind] = append(links[n.Kind], n.Link)
	}
	return links
}

func wantLink(t *testing.T, links map[string][]string, kind, want string) {
	t.Helper()
	if slices.Contains(links[kind], want) {
		return
	}
	t.Fatalf("%s links = %v, want %q", kind, links[kind], want)
}

// Links are web app paths (emailed as PUBLIC_WEB_URL + link), so each one must open the page the
// recipient actually uses in biletflow-frontend.
func TestNotificationLinksOpenWebPages(t *testing.T) {
	e := newDBServer(t)
	owner := createOrganizerClient(t, e, "links-owner")
	eventID := createEvent(t, owner, eventInput("public"))
	typeID := createTicketType(t, owner, eventID, ticketTypeInput("Free", 5))
	owner.do(http.MethodPost, "/api/v1/organizer/events/"+eventID+"/publish", nil, http.StatusOK, nil)

	attendee := signInWithCode(t, e, uniqueEmail("links-attendee"))
	var reg registrationResponse
	attendee.do(http.MethodPost, "/api/v1/events/"+eventID+"/register", registration(typeID, 1), http.StatusCreated, &reg)
	links := notificationLinks(attendee)
	wantLink(t, links, store.KindOrderConfirmed, "/account/orders/"+reg.OrderID)
	wantLink(t, links, store.KindTickets, "/account/orders/"+reg.OrderID)

	var opened struct {
		Case store.SupportCase `json:"case"`
	}
	attendee.do(http.MethodPost, "/api/v1/support/cases", map[string]any{
		"kind": "attendee", "category": "ticket_delivery", "subject": "Where is my ticket?",
		"message": "I can't find the PDF.", "orderId": reg.OrderID,
	}, http.StatusCreated, &opened)
	caseID := opened.Case.ID
	wantLink(t, notificationLinks(owner), "support_message", "/organizer/events/"+eventID+"/support/"+caseID)

	owner.do(http.MethodPost, "/api/v1/support/cases/"+caseID+"/messages",
		map[string]string{"body": "It's attached to your order."}, http.StatusCreated, nil)
	wantLink(t, notificationLinks(attendee), "support_message", "/account/support/"+caseID)
}
