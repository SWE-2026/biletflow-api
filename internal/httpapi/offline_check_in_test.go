package httpapi

import (
	"bytes"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/SWE-2026/biletflow-api/internal/store"
	"github.com/labstack/echo/v4"
)

type offlineSyncResponse struct {
	Results []store.OfflineScanResult `json:"results"`
}

// offlineFixture is a published free event with issued tickets and two assigned scanners.
type offlineFixture struct {
	eventID  string
	tokens   []string
	scanners [2]client
}

func newOfflineFixture(t *testing.T, e *echo.Echo, name string, tickets int) offlineFixture {
	t.Helper()
	owner := createOrganizerClient(t, e, name+"-owner")
	f := offlineFixture{eventID: createEvent(t, owner, eventInput("public"))}
	typeID := createTicketType(t, owner, f.eventID, ticketTypeInput("General", tickets))
	owner.do(http.MethodPost, "/api/v1/organizer/events/"+f.eventID+"/publish", nil, http.StatusOK, nil)
	attendee := signInWithCode(t, e, uniqueEmail(name+"-attendee"))
	var issued registrationResponse
	attendee.do(http.MethodPost, "/api/v1/events/"+f.eventID+"/register", registration(typeID, tickets), http.StatusCreated, &issued)
	for _, tk := range issued.Tickets {
		f.tokens = append(f.tokens, tk.QRToken)
	}
	for i := range f.scanners {
		address := uniqueEmail(name + "-scanner")
		f.scanners[i] = signInWithCode(t, e, address)
		owner.do(http.MethodPost, "/api/v1/organizer/events/"+f.eventID+"/staff",
			map[string]string{"email": address, "role": "scanner"}, http.StatusOK, nil)
	}
	return f
}

func (f offlineFixture) path(suffix string) string {
	return "/api/v1/staff/events/" + f.eventID + suffix
}

func newScanID(t *testing.T) string {
	t.Helper()
	b := make([]byte, 16)
	if _, err := rand.Read(b); err != nil {
		t.Fatal(err)
	}
	h := hex.EncodeToString(b)
	return fmt.Sprintf("%s-%s-%s-%s-%s", h[:8], h[8:12], h[12:16], h[16:20], h[20:])
}

func tokenHash(token string) string {
	sum := sha256.Sum256([]byte(token))
	return hex.EncodeToString(sum[:])
}

func TestOfflineManifest(t *testing.T) {
	e := newDBServer(t)
	f := newOfflineFixture(t, e, "offline-manifest", 3)

	var raw map[string]json.RawMessage
	f.scanners[0].do(http.MethodGet, f.path("/offline-manifest"), nil, http.StatusOK, &raw)
	if strings.Contains(string(raw["tickets"]), f.tokens[0]) {
		t.Fatal("the manifest must not contain QR tokens")
	}
	var m store.OfflineManifest
	f.scanners[0].do(http.MethodGet, f.path("/offline-manifest"), nil, http.StatusOK, &m)
	if len(m.Tickets) != len(f.tokens) || m.GeneratedAt.IsZero() {
		t.Fatalf("manifest = %d tickets at %v, want %d", len(m.Tickets), m.GeneratedAt, len(f.tokens))
	}
	hashes := map[string]bool{}
	for _, tk := range m.Tickets {
		hashes[tk.TokenHash] = tk.Status == "valid" && tk.ID != ""
	}
	for i, token := range f.tokens {
		if !hashes[tokenHash(token)] {
			t.Fatalf("no valid ticket with the SHA-256 of token %d", i)
		}
	}

	outsider := signInWithCode(t, e, uniqueEmail("offline-outsider"))
	outsider.do(http.MethodGet, f.path("/offline-manifest"), nil, http.StatusNotFound, nil)
}

func TestOfflineSync(t *testing.T) {
	e := newDBServer(t)
	f := newOfflineFixture(t, e, "offline-sync", 2)
	scanner := f.scanners[0]
	scannedAt := time.Now().Add(-10 * time.Minute).UTC().Truncate(time.Second)
	first := newScanID(t)

	var got offlineSyncResponse
	scanner.do(http.MethodPost, f.path("/offline-check-ins"), offlineSyncInput{Scans: []store.OfflineScan{
		{ScanID: first, QRToken: f.tokens[0], ScannedAt: scannedAt},
		{ScanID: newScanID(t), QRToken: f.tokens[0], ScannedAt: scannedAt.Add(time.Minute)},
		{ScanID: newScanID(t), QRToken: "not-a-ticket", ScannedAt: scannedAt},
		{ScanID: newScanID(t), QRToken: "https://biletflow.kz/e/x?campaign=abc", ScannedAt: scannedAt},
		{ScanID: newScanID(t), QRToken: f.tokens[1], ScannedAt: time.Now().Add(time.Hour)},
	}}, http.StatusOK, &got)

	want := []string{"valid", "already_used", "invalid", "campaign_qr", "valid"}
	if len(got.Results) != len(want) {
		t.Fatalf("got %d results, want %d", len(got.Results), len(want))
	}
	for i, w := range want {
		if got.Results[i].Result != w {
			t.Fatalf("result %d = %q, want %q", i, got.Results[i].Result, w)
		}
	}
	if got.Results[0].ScanID != first || !got.Results[0].Ticket.CheckedInAt.Equal(scannedAt) {
		t.Fatalf("first result = %+v, want scan %s checked in at %v", got.Results[0], first, scannedAt)
	}
	if got.Results[4].Ticket.CheckedInAt.After(time.Now()) {
		t.Fatal("a scan time in the future must be recorded as now")
	}

	// Retrying a sync whose response was lost returns the first outcome, not a duplicate.
	var retry offlineSyncResponse
	scanner.do(http.MethodPost, f.path("/offline-check-ins"), offlineSyncInput{Scans: []store.OfflineScan{
		{ScanID: first, QRToken: f.tokens[0], ScannedAt: scannedAt},
	}}, http.StatusOK, &retry)
	if retry.Results[0].Result != "valid" {
		t.Fatalf("retried scan = %q, want valid", retry.Results[0].Result)
	}
}

// TestLiveScanIDReplay covers a live scan whose response was lost: the scanner queued it offline
// under the same scan ID, and the sync must not report the scanner's own check-in as a duplicate.
func TestLiveScanIDReplay(t *testing.T) {
	e := newDBServer(t)
	f := newOfflineFixture(t, e, "offline-replay", 1)
	scanID := newScanID(t)
	var live store.CheckInResult
	f.scanners[0].do(http.MethodPost, f.path("/check-in"),
		map[string]string{"qrToken": f.tokens[0], "scanId": scanID}, http.StatusOK, &live)
	var synced offlineSyncResponse
	f.scanners[0].do(http.MethodPost, f.path("/offline-check-ins"), offlineSyncInput{Scans: []store.OfflineScan{
		{ScanID: scanID, QRToken: f.tokens[0], ScannedAt: time.Now()},
	}}, http.StatusOK, &synced)
	if live.Result != "valid" || synced.Results[0].Result != "valid" {
		t.Fatalf("live = %q, synced = %q, want valid twice", live.Result, synced.Results[0].Result)
	}
	// The other scanner's own scan of the same ticket is a real duplicate.
	var other store.CheckInResult
	f.scanners[1].do(http.MethodPost, f.path("/check-in"),
		map[string]string{"qrToken": f.tokens[0], "scanId": scanID}, http.StatusOK, &other)
	if other.Result != "already_used" {
		t.Fatalf("other scanner = %q, want already_used", other.Result)
	}
}

// TestOfflineSyncRace syncs the same ticket from two offline scanners at once: one admits it.
func TestOfflineSyncRace(t *testing.T) {
	e := newDBServer(t)
	f := newOfflineFixture(t, e, "offline-race", 1)
	results := make(chan string, len(f.scanners))
	var workers sync.WaitGroup
	for _, scanner := range f.scanners {
		body, err := json.Marshal(offlineSyncInput{Scans: []store.OfflineScan{
			{ScanID: newScanID(t), QRToken: f.tokens[0], ScannedAt: time.Now()},
		}})
		if err != nil {
			t.Fatal(err)
		}
		workers.Add(1)
		go func() {
			defer workers.Done()
			req := httptest.NewRequest(http.MethodPost, f.path("/offline-check-ins"), bytes.NewReader(body))
			req.Header.Set(echo.HeaderAuthorization, "Bearer "+scanner.token)
			req.Header.Set(echo.HeaderContentType, echo.MIMEApplicationJSON)
			rec := httptest.NewRecorder()
			e.ServeHTTP(rec, req)
			var got offlineSyncResponse
			if rec.Code != http.StatusOK || json.Unmarshal(rec.Body.Bytes(), &got) != nil || len(got.Results) != 1 {
				results <- fmt.Sprintf("status %d", rec.Code)
				return
			}
			results <- got.Results[0].Result
		}()
	}
	workers.Wait()
	close(results)
	counts := map[string]int{}
	for r := range results {
		counts[r]++
	}
	if counts["valid"] != 1 || counts["already_used"] != 1 {
		t.Fatalf("concurrent sync results = %v, want one valid and one already_used", counts)
	}
}

func TestValidateOfflineScans(t *testing.T) {
	t.Parallel()
	id := "0b7c8f3e-1d2a-4c5b-9e8f-7a6b5c4d3e2f"
	scan := store.OfflineScan{ScanID: id, QRToken: "token", ScannedAt: time.Now()}
	with := func(edit func(*store.OfflineScan)) []store.OfflineScan {
		s := scan
		edit(&s)
		return []store.OfflineScan{s}
	}
	tests := []struct {
		name      string
		give      []store.OfflineScan
		wantError bool
	}{
		{name: "valid", give: []store.OfflineScan{scan}},
		{name: "upper-case scan ID", give: with(func(s *store.OfflineScan) { s.ScanID = strings.ToUpper(id) })},
		{name: "empty batch", give: nil, wantError: true},
		{name: "too many", give: make([]store.OfflineScan, maxOfflineScans+1), wantError: true},
		{name: "bad scan ID", give: with(func(s *store.OfflineScan) { s.ScanID = "scan-1" }), wantError: true},
		{name: "repeated scan ID", give: []store.OfflineScan{scan, scan}, wantError: true},
		{name: "blank token", give: with(func(s *store.OfflineScan) { s.QRToken = "  " }), wantError: true},
		{name: "long token", give: with(func(s *store.OfflineScan) { s.QRToken = strings.Repeat("a", maxQRTokenLength+1) }), wantError: true},
		{name: "no scan time", give: with(func(s *store.OfflineScan) { s.ScannedAt = time.Time{} }), wantError: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			if err := validateOfflineScans(tt.give); (err != nil) != tt.wantError {
				t.Fatalf("validateOfflineScans() error = %v, wantError %v", err, tt.wantError)
			}
		})
	}
}
