package httpapi

import (
	"errors"
	"log/slog"
	"net/http"

	"github.com/SWE-2026/biletflow-api/internal/auth"
	"github.com/SWE-2026/biletflow-api/internal/store"
	"github.com/labstack/echo/v4"
)

// Error is the API's single error shape: {"code": "...", "message": "...", "fields": {...}}.
// Code is stable and meant for clients to branch on; Message is for people.
type Error struct {
	Status  int               `json:"-"`
	Code    string            `json:"code"`
	Message string            `json:"message"`
	Fields  map[string]string `json:"fields,omitempty"`
}

func (e *Error) Error() string { return e.Code + ": " + e.Message }

func newError(status int, code, message string) *Error {
	return &Error{Status: status, Code: code, Message: message}
}

var (
	errInternal     = newError(http.StatusInternalServerError, "internal", "Something went wrong. Please try again.")
	errNotFound     = newError(http.StatusNotFound, "not_found", "Not found.")
	errInvalidJSON  = newError(http.StatusBadRequest, "invalid_json", "The request body is not valid JSON for this endpoint.")
	errUnauthorized = newError(http.StatusUnauthorized, "unauthorized", "Sign in to continue.")
	errForbidden    = newError(http.StatusForbidden, "forbidden", "You do not have access to this action.")
)

// knownErrors maps domain errors from lower layers to API errors, so handlers can return them as is.
var knownErrors = []struct {
	err   error
	apiEr *Error
}{
	{store.ErrNotFound, errNotFound},
	{store.ErrEmailUnverified, newError(http.StatusForbidden, "email_unverified", "Verify your email before creating an organizer profile.")},
	{store.ErrRoleConflict, newError(http.StatusConflict, "role_conflict", "This account cannot create an organizer profile.")},
	{store.ErrInvalidEventState, newError(http.StatusConflict, "invalid_event_state", "This action is not allowed for the event's current status.")},
	{store.ErrCapacityExceeded, newError(http.StatusConflict, "capacity_exceeded", "Ticket quantities would exceed the event capacity.")},
	{store.ErrTicketTypeInUse, newError(http.StatusConflict, "ticket_type_in_use", "This ticket type already has orders and cannot be deleted.")},
	{store.ErrQuantityBelowSold, newError(http.StatusConflict, "quantity_below_sold", "Quantity cannot be lower than the tickets already issued.")},
	{store.ErrPaymentRequired, newError(http.StatusConflict, "payment_required", "This ticket type is paid. Buy it through checkout.")},
	{store.ErrRegistrationClosed, newError(http.StatusConflict, "registration_closed", "Registration for this ticket type is not open.")},
	{store.ErrTicketLimit, newError(http.StatusConflict, "ticket_limit_reached", "You have reached the ticket limit for this ticket type.")},
	{store.ErrSoldOut, newError(http.StatusConflict, "sold_out", "Not enough tickets are left.")},
	{store.ErrPaidSalesInactive, newError(http.StatusConflict, "paid_sales_inactive", "Paid ticket sales are not active for this event.")},
	{store.ErrPaidSalesSuspended, newError(http.StatusConflict, "paid_sales_suspended", "Paid sales for this event are suspended by the platform.")},
	{store.ErrActivationIncomplete, newError(http.StatusConflict, "activation_incomplete", "Complete the paid sales checklist before activating.")},
	{store.ErrOrderState, newError(http.StatusConflict, "invalid_order_state", "This action is not allowed for the order's current status.")},
	{store.ErrOrderExpired, newError(http.StatusConflict, "order_expired", "Your ticket hold has expired. Start checkout again.")},
	{store.ErrFreeOrder, newError(http.StatusConflict, "free_order", "Free registrations are cancelled, not refunded.")},
	{store.ErrForbidden, errForbidden},
	{store.ErrStateConflict, newError(http.StatusConflict, "invalid_state", "This action is not allowed in the current state.")},
	{store.ErrAlreadyDisputed, newError(http.StatusConflict, "already_disputed", "This order already has an open dispute.")},
	{store.ErrAlreadyReported, newError(http.StatusConflict, "already_reported", "You have already reported this event. The platform team will review it.")},
	{store.ErrInvalidAssignee, &Error{Status: http.StatusBadRequest, Code: "validation_failed", Message: "Some fields are invalid.",
		Fields: map[string]string{"assigneeId": "This person cannot handle this case."}}},
	{store.ErrCampaignTicketTypes, &Error{Status: http.StatusBadRequest, Code: "validation_failed", Message: "Some fields are invalid.",
		Fields: map[string]string{"ticketTypeIds": "Choose ticket types of this event."}}},
	{store.ErrPromoNotFound, newError(http.StatusConflict, "promo_not_found", "This promo code does not exist for this event.")},
	{store.ErrPromoDisabled, newError(http.StatusConflict, "promo_disabled", "This promo code has been disabled.")},
	{store.ErrPromoNotStarted, newError(http.StatusConflict, "promo_not_started", "This promo code is not active yet.")},
	{store.ErrPromoExpired, newError(http.StatusConflict, "promo_expired", "This promo code has expired.")},
	{store.ErrPromoExhausted, newError(http.StatusConflict, "promo_exhausted", "This promo code has reached its redemption limit.")},
	{store.ErrPromoNotApplicable, newError(http.StatusConflict, "promo_not_applicable", "This promo code does not apply to the selected tickets.")},
	{store.ErrUserNotFound, newError(http.StatusNotFound, "user_not_found", "No account uses this email. Ask the person to sign in to BiletFlow once first.")},
	{store.ErrNotCheckedIn, newError(http.StatusConflict, "not_checked_in", "This ticket is not checked in.")},
	{store.ErrRefundRequired, newError(http.StatusConflict, "refund_required", "Paid orders must be refunded instead of cancelled.")},
	{auth.ErrResendTooSoon, newError(http.StatusTooManyRequests, "resend_too_soon", "Please wait before requesting another code.")},
	{auth.ErrCodeExpired, newError(http.StatusGone, "code_expired", "This code has expired. Request a new one.")},
	{auth.ErrTooManyAttempts, newError(http.StatusTooManyRequests, "too_many_attempts", "Too many attempts. Request a new code.")},
	{auth.ErrInvalidCode, newError(http.StatusBadRequest, "invalid_code", "That code is incorrect. Check it and try again.")},
}

// errorHandler writes every error as an Error. Unknown errors are logged and returned as a generic
// 500, so internal details never reach clients.
func errorHandler(logger *slog.Logger) echo.HTTPErrorHandler {
	return func(err error, c echo.Context) {
		if c.Response().Committed {
			return
		}
		apiErr := toAPIError(err)
		if apiErr == errInternal {
			logger.ErrorContext(c.Request().Context(), "request failed",
				"method", c.Request().Method, "path", c.Path(), "error", err)
		}
		if writeErr := c.JSON(apiErr.Status, apiErr); writeErr != nil {
			logger.Error("write error response", "error", writeErr)
		}
	}
}

func toAPIError(err error) *Error {
	var apiErr *Error
	if errors.As(err, &apiErr) {
		return apiErr
	}
	var httpErr *echo.HTTPError
	if errors.As(err, &httpErr) {
		return fromEchoError(httpErr)
	}
	for _, known := range knownErrors {
		if errors.Is(err, known.err) {
			return known.apiEr
		}
	}
	return errInternal
}

func fromEchoError(e *echo.HTTPError) *Error {
	switch e.Code {
	case http.StatusNotFound:
		return errNotFound
	case http.StatusMethodNotAllowed:
		return newError(e.Code, "method_not_allowed", "Method not allowed.")
	case http.StatusRequestEntityTooLarge:
		return newError(e.Code, "body_too_large", "The request body is too large.")
	case http.StatusTooManyRequests:
		return newError(e.Code, "rate_limited", "Too many requests. Slow down and try again.")
	default:
		if e.Code >= 500 {
			return errInternal
		}
		return newError(e.Code, "bad_request", http.StatusText(e.Code))
	}
}
