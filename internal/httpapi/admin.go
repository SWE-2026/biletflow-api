package httpapi

import (
	"net/http"
	"slices"
	"strings"

	"github.com/SWE-2026/biletflow-api/internal/store"
	"github.com/labstack/echo/v4"
)

var (
	orderStatuses      = []string{"pending", "paid", "cancelled", "refunded"}
	paymentStatuses    = []string{"failed", "succeeded", "refunded"}
	eventStatuses      = []string{"draft", "published", "cancelled", "suspended"}
	activationStatuses = []string{"pending", "active", "suspended", "rejected"}
	reportStatuses     = []string{"open", "dismissed", "actioned"}
	reportReasons      = []string{"fraud", "inappropriate", "misleading", "other"}
)

func (s *Server) requireAdmin(next echo.HandlerFunc) echo.HandlerFunc {
	return func(c echo.Context) error {
		if currentUser(c).Role != "admin" {
			return errForbidden
		}
		return next(c)
	}
}

func (s *Server) adminRoutes(admin *echo.Group) {
	admin.GET("/users", s.adminUsers)
	admin.POST("/users/:userId/suspend", s.suspendUser)
	admin.POST("/users/:userId/restore", s.restoreUser)
	admin.GET("/events", s.adminEvents)
	admin.GET("/events/:id/activity", s.adminActivity)
	admin.POST("/events/:id/suspend", s.suspendEvent)
	admin.POST("/events/:id/restore", s.restoreEvent)
	admin.POST("/events/:id/paid-sales/approve", s.approvePaidSales)
	admin.POST("/events/:id/paid-sales/reject", s.rejectPaidSales)
	admin.POST("/events/:id/paid-sales/suspend", s.suspendPaidSales)
	admin.POST("/events/:id/paid-sales/restore", s.restorePaidSales)
	admin.GET("/orders", s.adminOrders)
	admin.GET("/payments", s.adminPayments)
	admin.GET("/refunds", s.adminRefunds)
	admin.GET("/disputes", s.adminDisputes)
	admin.GET("/payouts", s.adminPayouts)
	admin.POST("/payouts/schedule", s.schedulePayouts)
	admin.POST("/payouts/:payoutId/mark-paid", s.markPayoutPaid)
	admin.POST("/payouts/:payoutId/hold", s.holdPayout)
	admin.POST("/payouts/:payoutId/release", s.releasePayout)
	admin.POST("/disputes/:disputeId/resolve", s.resolveDispute)
	admin.GET("/paid-sales", s.adminActivations)
	admin.GET("/campaigns", s.adminCampaigns)
	admin.GET("/reports", s.adminReports)
	admin.POST("/reports/:reportId/resolve", s.resolveReport)
	admin.GET("/settings", s.getSettings)
	admin.PUT("/settings", s.updateSettings)
	admin.GET("/export/:kind", s.exportReport)
}

func searchParams(c echo.Context, statuses []string) (string, string, error) {
	query := strings.TrimSpace(c.QueryParam("q"))
	status := c.QueryParam("status")
	fe := fieldErrors{}
	fe.check(len(query) <= 100, "q", "Use up to 100 characters.")
	fe.check(status == "" || slices.Contains(statuses, status), "status", "Choose a valid status.")
	return query, status, fe.err()
}

func listJSON[T any](c echo.Context, key string, items []T, err error) error {
	if err != nil {
		return err
	}
	if items == nil {
		items = []T{}
	}
	return c.JSON(http.StatusOK, map[string]any{key: items})
}

func (s *Server) adminUsers(c echo.Context) error {
	query, _, err := searchParams(c, nil)
	if err != nil {
		return err
	}
	users, err := s.db.AdminUsers(c.Request().Context(), query)
	return listJSON(c, "users", users, err)
}

func (s *Server) adminEvents(c echo.Context) error {
	query, status, err := searchParams(c, eventStatuses)
	if err != nil {
		return err
	}
	events, err := s.db.AdminEvents(c.Request().Context(), query, status)
	return listJSON(c, "events", events, err)
}

func (s *Server) adminOrders(c echo.Context) error {
	query, status, err := searchParams(c, orderStatuses)
	if err != nil {
		return err
	}
	orders, err := s.db.AdminOrders(c.Request().Context(), query, status, store.AdminListLimit)
	return listJSON(c, "orders", orders, err)
}

func (s *Server) adminPayments(c echo.Context) error {
	query, status, err := searchParams(c, paymentStatuses)
	if err != nil {
		return err
	}
	payments, err := s.db.AdminPayments(c.Request().Context(), query, status, store.AdminListLimit)
	return listJSON(c, "payments", payments, err)
}

func (s *Server) adminRefunds(c echo.Context) error {
	refunds, err := s.db.AdminRefunds(c.Request().Context(), store.AdminListLimit)
	return listJSON(c, "refunds", refunds, err)
}

func (s *Server) adminActivations(c echo.Context) error {
	_, status, err := searchParams(c, activationStatuses)
	if err != nil {
		return err
	}
	activations, err := s.db.AdminActivations(c.Request().Context(), status)
	return listJSON(c, "paidSales", activations, err)
}

func (s *Server) adminCampaigns(c echo.Context) error {
	campaigns, err := s.db.AdminCampaigns(c.Request().Context())
	return listJSON(c, "campaigns", campaigns, err)
}

func (s *Server) adminReports(c echo.Context) error {
	_, status, err := searchParams(c, reportStatuses)
	if err != nil {
		return err
	}
	reports, err := s.db.EventReports(c.Request().Context(), status)
	return listJSON(c, "reports", reports, err)
}

func (s *Server) adminActivity(c echo.Context) error {
	id, err := eventID(c)
	if err != nil {
		return err
	}
	fe := fieldErrors{}
	var f store.ActivityFilter
	f.From, f.To = dateRange(c, fe)
	f.Type = c.QueryParam("type")
	fe.check(f.Type == "" || slices.Contains(store.ActivityTypes, f.Type), "type", "Choose a valid activity type.")
	if err := fe.err(); err != nil {
		return err
	}
	entries, err := s.db.AdminActivity(c.Request().Context(), id, f)
	return listJSON(c, "activity", entries, err)
}
