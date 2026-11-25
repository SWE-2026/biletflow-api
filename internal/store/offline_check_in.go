package store

import (
	"context"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
)

// OfflineTicket is a StaffTicket with the SHA-256 (lowercase hex) of its QR token, so a scanner
// can recognise the ticket without a connection. It never contains the token itself.
type OfflineTicket struct {
	StaffTicket
	TokenHash string `json:"tokenHash"`
}

// OfflineManifest is every ticket of an event, as downloaded by a scanner before going offline.
type OfflineManifest struct {
	GeneratedAt time.Time       `json:"generatedAt"`
	Tickets     []OfflineTicket `json:"tickets"`
}

// OfflineScan is one ticket admitted by a scanner while offline, replayed when it reconnects.
type OfflineScan struct {
	ScanID    string    `json:"scanId"`
	QRToken   string    `json:"qrToken"`
	ScannedAt time.Time `json:"scannedAt"`
}

// OfflineScanResult is the server's outcome for one OfflineScan.
type OfflineScanResult struct {
	ScanID string `json:"scanId"`
	CheckInResult
}

// offlineTicketSelect is staffTicketSelect with the token hash as the first column.
var offlineTicketSelect = strings.Replace(staffTicketSelect,
	"SELECT ", "SELECT encode(sha256(convert_to(t.qr_token, 'UTF8')), 'hex'), ", 1)

// OfflineManifest lists the event's tickets of every status for an offline scanner. Like
// check-in, it needs staff access and a published event.
func (p *Postgres) OfflineManifest(ctx context.Context, userID, eventID string) (OfflineManifest, error) {
	_, status, err := staffRole(ctx, p.pool, userID, eventID)
	if err != nil {
		return OfflineManifest{}, err
	}
	if status != "published" {
		return OfflineManifest{}, ErrInvalidEventState
	}
	m := OfflineManifest{GeneratedAt: time.Now().UTC()}
	rows, err := p.pool.Query(ctx, offlineTicketSelect+` ORDER BY t.id`, eventID)
	if err != nil {
		return OfflineManifest{}, err
	}
	m.Tickets, err = pgx.CollectRows(rows, func(row pgx.CollectableRow) (OfflineTicket, error) {
		var t OfflineTicket
		err := row.Scan(&t.TokenHash, &t.ID, &t.Status, &t.TicketTypeName, &t.AttendeeName,
			&t.AttendeeEmail, &t.OrderID, &t.CheckedInAt)
		return t, err
	})
	if m.Tickets == nil {
		m.Tickets = []OfflineTicket{}
	}
	return m, err
}

// SyncOfflineCheckIns replays offline scans in order, each in its own transaction, so the first
// scan of a ticket to reach the server admits it and later ones come back as already_used. A
// scan ID seen before returns its first outcome, so a retried sync is safe. A scan time in the
// future is recorded as now.
func (p *Postgres) SyncOfflineCheckIns(ctx context.Context, userID, eventID string, scans []OfflineScan) ([]OfflineScanResult, error) {
	results := make([]OfflineScanResult, 0, len(scans))
	for _, s := range scans {
		at := s.ScannedAt
		if now := time.Now(); at.After(now) {
			at = now
		}
		res, err := p.checkIn(ctx, userID, eventID, byToken, s.QRToken, scanMeta{id: s.ScanID, at: &at})
		if err != nil {
			return nil, err
		}
		results = append(results, OfflineScanResult{ScanID: s.ScanID, CheckInResult: res})
	}
	return results, nil
}
