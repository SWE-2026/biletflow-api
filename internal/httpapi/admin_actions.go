package httpapi

import (
	"encoding/csv"
	"net/http"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/SWE-2026/biletflow-api/internal/store"
	"github.com/labstack/echo/v4"
)

func (s *Server) suspendUser(c echo.Context) error { return s.setUserSuspended(c, true) }

func (s *Server) restoreUser(c echo.Context) error { return s.setUserSuspended(c, false) }

func (s *Server) setUserSuspended(c echo.Context, suspend bool) error {
	userID, err := pathID(c, "userId")
	if err != nil {
		return err
	}
	u, err := s.db.SetUserSuspended(c.Request().Context(), currentUser(c).ID, userID, suspend)
	if err != nil {
		return err
	}
	return c.JSON(http.StatusOK, map[string]any{"user": u})
}

func (s *Server) suspendEvent(c echo.Context) error { return s.setEventSuspended(c, true) }

func (s *Server) restoreEvent(c echo.Context) error { return s.setEventSuspended(c, false) }

func (s *Server) setEventSuspended(c echo.Context, suspend bool) error {
	id, err := eventID(c)
	if err != nil {
		return err
	}
	e, err := s.db.SetEventSuspended(c.Request().Context(), currentUser(c).ID, id, suspend)
	if err != nil {
		return err
	}
	return c.JSON(http.StatusOK, map[string]any{"event": e})
}

func (s *Server) suspendPaidSales(c echo.Context) error { return s.setPaidSalesSuspended(c, true) }

func (s *Server) restorePaidSales(c echo.Context) error { return s.setPaidSalesSuspended(c, false) }

func (s *Server) setPaidSalesSuspended(c echo.Context, suspend bool) error {
	id, err := eventID(c)
	if err != nil {
		return err
	}
	if err := s.db.SetPaidSalesSuspended(c.Request().Context(), currentUser(c).ID, id, suspend); err != nil {
		return err
	}
	return c.NoContent(http.StatusNoContent)
}

type resolveReportInput struct {
	Status string `json:"status"`
}

func (s *Server) resolveReport(c echo.Context) error {
	reportID, err := pathID(c, "reportId")
	if err != nil {
		return err
	}
	var in resolveReportInput
	if err := decodeJSON(c, &in); err != nil {
		return err
	}
	fe := fieldErrors{}
	fe.check(in.Status == "dismissed" || in.Status == "actioned", "status", "Choose dismissed or actioned.")
	if err := fe.err(); err != nil {
		return err
	}
	r, err := s.db.ResolveReport(c.Request().Context(), currentUser(c).ID, reportID, in.Status)
	if err != nil {
		return err
	}
	return c.JSON(http.StatusOK, map[string]any{"report": r})
}

type reportInput struct {
	Reason  string `json:"reason"`
	Details string `json:"details"`
}

func (s *Server) reportEvent(c echo.Context) error {
	id, err := eventID(c)
	if err != nil {
		return err
	}
	var in reportInput
	if err := decodeJSON(c, &in); err != nil {
		return err
	}
	in.Details = strings.TrimSpace(in.Details)
	fe := fieldErrors{}
	fe.check(slices.Contains(reportReasons, in.Reason), "reason", "Choose fraud, inappropriate, misleading or other.")
	fe.check(len(in.Details) <= 2000, "details", "Use up to 2000 characters.")
	if err := fe.err(); err != nil {
		return err
	}
	if err := s.db.ReportEvent(c.Request().Context(), currentUser(c).ID, id, in.Reason, in.Details); err != nil {
		return err
	}
	return c.NoContent(http.StatusNoContent)
}

func (s *Server) getSettings(c echo.Context) error {
	settings, err := s.db.Settings(c.Request().Context())
	if err != nil {
		return err
	}
	return c.JSON(http.StatusOK, map[string]any{"settings": settings})
}

func (s *Server) updateSettings(c echo.Context) error {
	var in struct {
		ActivationFeeAmount  int64 `json:"activationFeeAmount"`
		ProcessingFeePercent int   `json:"processingFeePercent"`
	}
	if err := decodeJSON(c, &in); err != nil {
		return err
	}
	fe := fieldErrors{}
	fe.check(in.ActivationFeeAmount >= 0 && in.ActivationFeeAmount <= maxPriceAmount, "activationFeeAmount", "Choose a fee from 0 to 10000000 KZT.")
	fe.check(in.ProcessingFeePercent >= 0 && in.ProcessingFeePercent <= 20, "processingFeePercent", "Choose a percentage from 0 to 20.")
	if err := fe.err(); err != nil {
		return err
	}
	settings, err := s.db.UpdateSettings(c.Request().Context(), currentUser(c).ID, store.PlatformSettings{
		ActivationFeeAmount: in.ActivationFeeAmount, ProcessingFeePercent: in.ProcessingFeePercent})
	if err != nil {
		return err
	}
	return c.JSON(http.StatusOK, map[string]any{"settings": settings})
}

func csvCell(v string) string {
	if v != "" && strings.ContainsAny(v[:1], "=+-@\t\r") {
		return "'" + v
	}
	return v
}

func csvTime(t *time.Time) string {
	if t == nil {
		return ""
	}
	return t.UTC().Format(time.RFC3339)
}

func (s *Server) exportRows(c echo.Context, kind string) ([][]string, error) {
	ctx := c.Request().Context()
	itoa := func(n int64) string { return strconv.FormatInt(n, 10) }
	switch kind {
	case "orders":
		orders, err := s.db.AdminOrders(ctx, "", "", store.ExportLimit)
		rows := [][]string{{"order_id", "event", "buyer_email", "status", "subtotal_kzt", "discount_kzt", "total_kzt", "processing_fee_kzt", "created_at", "paid_at"}}
		for _, o := range orders {
			rows = append(rows, []string{o.ID, o.EventTitle, o.BuyerEmail, o.Status, itoa(o.SubtotalAmount), itoa(o.DiscountAmount),
				itoa(o.TotalAmount), itoa(o.ProcessingFeeAmount), csvTime(&o.CreatedAt), csvTime(o.PaidAt)})
		}
		return rows, err
	case "payments":
		payments, err := s.db.AdminPayments(ctx, "", "", store.ExportLimit)
		rows := [][]string{{"payment_id", "order_id", "event", "buyer_email", "amount_kzt", "status", "demo", "created_at"}}
		for _, p := range payments {
			rows = append(rows, []string{p.ID, p.OrderID, p.EventTitle, p.BuyerEmail, itoa(p.Amount), p.Status,
				strconv.FormatBool(p.Demo), csvTime(&p.CreatedAt)})
		}
		return rows, err
	default:
		refunds, err := s.db.AdminRefunds(ctx, store.ExportLimit)
		rows := [][]string{{"refund_id", "order_id", "event", "amount_kzt", "status", "reason", "demo", "created_at"}}
		for _, r := range refunds {
			reason := ""
			if r.Reason != nil {
				reason = *r.Reason
			}
			rows = append(rows, []string{r.ID, r.OrderID, r.EventTitle, itoa(r.Amount), r.Status, reason,
				strconv.FormatBool(r.Demo), csvTime(&r.CreatedAt)})
		}
		return rows, err
	}
}

func (s *Server) exportReport(c echo.Context) error {
	kind := c.Param("kind")
	if !slices.Contains([]string{"orders", "payments", "refunds"}, kind) {
		return errNotFound
	}
	rows, err := s.exportRows(c, kind)
	if err != nil {
		return err
	}
	c.Response().Header().Set(echo.HeaderContentType, "text/csv; charset=utf-8")
	c.Response().Header().Set(echo.HeaderContentDisposition, `attachment; filename="biletflow-`+kind+`.csv"`)
	c.Response().WriteHeader(http.StatusOK)
	w := csv.NewWriter(c.Response())
	for _, row := range rows {
		for i := range row {
			row[i] = csvCell(row[i])
		}
		if err := w.Write(row); err != nil {
			return err
		}
	}
	w.Flush()
	return w.Error()
}
