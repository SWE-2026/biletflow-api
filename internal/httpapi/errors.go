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
)

// knownErrors maps domain errors from lower layers to API errors, so handlers can return them as is.
var knownErrors = []struct {
	err   error
	apiEr *Error
}{
	{store.ErrNotFound, errNotFound},
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
