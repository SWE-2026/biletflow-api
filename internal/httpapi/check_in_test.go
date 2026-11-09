package httpapi

import (
	"net/http"
	"testing"

	"github.com/SWE-2026/biletflow-api/internal/store"
)

// TestAdmissionFlow protects the release-critical handoff between the web ticket flow and
// the Event Admin app: a valid issued ticket is admitted exactly once by an assigned scanner.
func TestAdmissionFlow(t *testing.T) {
	e := newDBServer(t)
	owner := createOrganizerClient(t, e, "admission-owner")
	eventID := createEvent(t, owner, eventInput("public"))
	typeID := createTicketType(t, owner, eventID, ticketTypeInput("General", 2))
	owner.do(http.MethodPost, "/api/v1/organizer/events/"+eventID+"/publish", nil, http.StatusOK, nil)

	attendee := signInWithCode(t, e, uniqueEmail("admission-attendee"))
	var issued registrationResponse
	attendee.do(http.MethodPost, "/api/v1/events/"+eventID+"/register", registration(typeID, 1), http.StatusCreated, &issued)
	if len(issued.Tickets) != 1 || issued.Tickets[0].QRToken == "" {
		t.Fatalf("registration did not issue one QR ticket: %+v", issued)
	}

	scannerAddress := uniqueEmail("admission-scanner")
	scanner := signInWithCode(t, e, scannerAddress)
	owner.do(http.MethodPost, "/api/v1/organizer/events/"+eventID+"/staff", map[string]string{
		"email": scannerAddress, "role": "scanner",
	}, http.StatusOK, nil)

	path := "/api/v1/staff/events/" + eventID + "/check-in"
	var first, duplicate store.CheckInResult
	scanner.do(http.MethodPost, path, map[string]string{"qrToken": issued.Tickets[0].QRToken}, http.StatusOK, &first)
	if first.Result != "valid" || first.Ticket == nil || first.Ticket.Status != "used" {
		t.Fatalf("first scan = %+v, want a used valid ticket", first)
	}
	scanner.do(http.MethodPost, path, map[string]string{"qrToken": issued.Tickets[0].QRToken}, http.StatusOK, &duplicate)
	if duplicate.Result != "already_used" || duplicate.Ticket == nil {
		t.Fatalf("second scan = %+v, want already_used", duplicate)
	}
}
