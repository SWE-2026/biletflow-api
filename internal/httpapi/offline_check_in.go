package httpapi

import (
	"fmt"
	"net/http"
	"strings"

	"github.com/SWE-2026/biletflow-api/internal/store"
	"github.com/labstack/echo/v4"
)

// maxOfflineScans bounds one sync request; scanners send longer queues in several requests.
const maxOfflineScans = 200

type offlineSyncInput struct {
	Scans []store.OfflineScan `json:"scans"`
}

func (s *Server) getOfflineManifest(c echo.Context) error {
	id, err := eventID(c)
	if err != nil {
		return err
	}
	m, err := s.db.OfflineManifest(c.Request().Context(), currentUser(c).ID, id)
	if err != nil {
		return err
	}
	return c.JSON(http.StatusOK, m)
}

func (s *Server) syncOfflineCheckIns(c echo.Context) error {
	id, err := eventID(c)
	if err != nil {
		return err
	}
	var in offlineSyncInput
	if err := decodeJSON(c, &in); err != nil {
		return err
	}
	if err := validateOfflineScans(in.Scans); err != nil {
		return err
	}
	results, err := s.db.SyncOfflineCheckIns(c.Request().Context(), currentUser(c).ID, id, in.Scans)
	if err != nil {
		return err
	}
	return c.JSON(http.StatusOK, map[string]any{"results": results})
}

// validateOfflineScans checks the batch's bounds and trims tokens in place. Errors are keyed
// `scans.<index>` so the scanner can tell which queued scan is malformed.
func validateOfflineScans(scans []store.OfflineScan) error {
	fe := fieldErrors{}
	fe.check(len(scans) > 0 && len(scans) <= maxOfflineScans, "scans",
		fmt.Sprintf("Send 1 to %d scans.", maxOfflineScans))
	seen := make(map[string]bool, len(scans))
	for i := range scans {
		sc := &scans[i]
		key := fmt.Sprintf("scans.%d", i)
		sc.QRToken = strings.TrimSpace(sc.QRToken)
		sc.ScanID = strings.ToLower(sc.ScanID)
		fe.check(eventIDPattern.MatchString(sc.ScanID), key, "scanId must be a UUID.")
		fe.check(!seen[sc.ScanID], key, "scanId is repeated in this request.")
		fe.check(len(sc.QRToken) > 0 && len(sc.QRToken) <= maxQRTokenLength, key, "qrToken is missing or too long.")
		fe.check(!sc.ScannedAt.IsZero(), key, "scannedAt is required.")
		seen[sc.ScanID] = true
	}
	return fe.err()
}
