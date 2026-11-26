package httpapi

import (
	"bytes"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/labstack/echo/v4"
)

func postEventImage(t *testing.T, c client, name string, data []byte) *httptest.ResponseRecorder {
	t.Helper()
	var buf bytes.Buffer
	w := multipart.NewWriter(&buf)
	part, err := w.CreateFormFile("file", name)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := part.Write(data); err != nil {
		t.Fatal(err)
	}
	if err := w.Close(); err != nil {
		t.Fatal(err)
	}
	req := httptest.NewRequest(http.MethodPost, "/api/v1/organizer/event-images", &buf)
	req.Header.Set(echo.HeaderContentType, w.FormDataContentType())
	req.Header.Set(echo.HeaderAuthorization, "Bearer "+c.token)
	rec := httptest.NewRecorder()
	c.e.ServeHTTP(rec, req)
	return rec
}

func TestEventImageUpload(t *testing.T) {
	e, _ := newDBServerWithFiles(t)
	owner := createOrganizerClient(t, e, "event-images")

	if rec := postEventImage(t, owner, "doc.pdf", []byte("%PDF-1.4 not an image")); rec.Code != http.StatusBadRequest {
		t.Fatalf("non-image: status %d; body %s", rec.Code, rec.Body.String())
	}
	attendee := signInWithCode(t, e, uniqueEmail("event-images-attendee"))
	if rec := postEventImage(t, attendee, "a.png", pngBytes); rec.Code == http.StatusCreated {
		t.Fatalf("non-organizer upload allowed")
	}

	rec := postEventImage(t, owner, "cover.png", pngBytes)
	if rec.Code != http.StatusCreated {
		t.Fatalf("upload: status %d; body %s", rec.Code, rec.Body.String())
	}
	body := rec.Body.String()
	start := strings.Index(body, "/api/v1/event-images/")
	if start < 0 {
		t.Fatalf("no image url in %s", body)
	}
	path := body[start:strings.LastIndex(body, `"`)]

	get := httptest.NewRecorder()
	e.ServeHTTP(get, httptest.NewRequest(http.MethodGet, path, nil))
	if get.Code != http.StatusOK || get.Header().Get(echo.HeaderContentType) != "image/png" || !bytes.Equal(get.Body.Bytes(), pngBytes) {
		t.Fatalf("get image: status %d type %q", get.Code, get.Header().Get(echo.HeaderContentType))
	}
	missing := httptest.NewRecorder()
	e.ServeHTTP(missing, httptest.NewRequest(http.MethodGet, "/api/v1/event-images/00000000-0000-0000-0000-000000000001/AAAAAAAAAAAAAAAAAAAAAAAAAA.png", nil))
	if missing.Code != http.StatusNotFound {
		t.Fatalf("missing image: status %d", missing.Code)
	}

	input := eventInput("public")
	url := "http://example.com" + path
	input.ImageURL = &url
	createEvent(t, owner, input)
}
