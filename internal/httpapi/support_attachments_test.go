package httpapi

import (
	"bytes"
	"encoding/json"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/SWE-2026/biletflow-api/internal/store"
	"github.com/labstack/echo/v4"
)

var pngBytes = []byte("\x89PNG\r\n\x1a\n\x00\x00\x00\rIHDR\x00\x00\x00\x01\x00\x00\x00\x01\x08\x06\x00\x00\x00\x1f\x15\xc4\x89")

func TestCleanFileName(t *testing.T) {
	t.Parallel()
	tests := []struct{ give, want string }{
		{give: "screenshot.png", want: "screenshot.png"},
		{give: "../../etc/passwd", want: "passwd"},
		{give: "bad\"name\r\n.pdf", want: "bad_name__.pdf"},
		{give: "", want: "attachment"},
	}
	for _, tt := range tests {
		t.Run(tt.give, func(t *testing.T) {
			t.Parallel()
			if got := cleanFileName(tt.give); got != tt.want {
				t.Fatalf("cleanFileName(%q) = %q, want %q", tt.give, got, tt.want)
			}
		})
	}
}

func postAttachment(t *testing.T, c client, caseID, body, name string, data []byte) *httptest.ResponseRecorder {
	t.Helper()
	var buf bytes.Buffer
	w := multipart.NewWriter(&buf)
	if err := w.WriteField("body", body); err != nil {
		t.Fatal(err)
	}
	part, err := w.CreateFormFile("files", name)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := part.Write(data); err != nil {
		t.Fatal(err)
	}
	if err := w.Close(); err != nil {
		t.Fatal(err)
	}
	req := httptest.NewRequest(http.MethodPost, "/api/v1/support/cases/"+caseID+"/messages", &buf)
	req.Header.Set(echo.HeaderContentType, w.FormDataContentType())
	req.Header.Set(echo.HeaderAuthorization, "Bearer "+c.token)
	rec := httptest.NewRecorder()
	c.e.ServeHTTP(rec, req)
	return rec
}

func TestSupportAttachments(t *testing.T) {
	e := newDBServer(t)
	owner := createOrganizerClient(t, e, "attachments")
	eventID := createEvent(t, owner, eventInput("public"))
	createTicketType(t, owner, eventID, ticketTypeInput("General", 5))
	owner.do(http.MethodPost, "/api/v1/organizer/events/"+eventID+"/publish", nil, http.StatusOK, nil)
	attendee := signInWithCode(t, e, uniqueEmail("attachments-attendee"))
	var opened struct {
		Case store.SupportCase `json:"case"`
	}
	attendee.do(http.MethodPost, "/api/v1/support/cases", map[string]any{
		"kind": "attendee", "category": "technical", "subject": "Broken page",
		"message": "See the screenshot.", "eventId": eventID,
	}, http.StatusCreated, &opened)
	caseID := opened.Case.ID

	if rec := postAttachment(t, attendee, caseID, "", "tool.exe", []byte("MZ\x90\x00binary")); rec.Code != http.StatusBadRequest {
		t.Fatalf("unsupported file: status %d; body %s", rec.Code, rec.Body.String())
	}
	if rec := postAttachment(t, attendee, caseID, "Here it is", "screen.png", pngBytes); rec.Code != http.StatusCreated {
		t.Fatalf("upload: status %d; body %s", rec.Code, rec.Body.String())
	}
	var thread struct {
		Messages []store.SupportMessage `json:"messages"`
	}
	owner.do(http.MethodGet, "/api/v1/support/cases/"+caseID, nil, http.StatusOK, &thread)
	last := thread.Messages[len(thread.Messages)-1]
	if len(last.Attachments) != 1 || last.Attachments[0].ContentType != "image/png" || last.Attachments[0].FileName != "screen.png" {
		t.Fatalf("attachment not listed: %+v", last)
	}
	path := "/api/v1/support/cases/" + caseID + "/attachments/" + last.Attachments[0].ID
	req := httptest.NewRequest(http.MethodGet, path, nil)
	req.Header.Set(echo.HeaderAuthorization, "Bearer "+owner.token)
	rec := httptest.NewRecorder()
	e.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK || !bytes.Equal(rec.Body.Bytes(), pngBytes) || rec.Header().Get("X-Content-Type-Options") != "nosniff" {
		t.Fatalf("download: status %d, headers %v", rec.Code, rec.Header())
	}
	stranger := signInWithCode(t, e, uniqueEmail("attachments-stranger"))
	var notFound json.RawMessage
	stranger.do(http.MethodGet, path, nil, http.StatusNotFound, &notFound)
}
