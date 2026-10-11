package httpapi

import (
	"encoding/json"
	"net/http"
	netmail "net/mail"
	"strings"

	"github.com/labstack/echo/v4"
)

// decodeJSON reads the body into dst, rejecting malformed JSON and unknown fields.
func decodeJSON(c echo.Context, dst any) error {
	dec := json.NewDecoder(c.Request().Body)
	dec.DisallowUnknownFields()
	if err := dec.Decode(dst); err != nil {
		return errInvalidJSON
	}
	return nil
}

// fieldErrors collects validation failures keyed by JSON field name; the first message per field wins.
type fieldErrors map[string]string

func (f fieldErrors) check(ok bool, field, message string) {
	if _, seen := f[field]; !ok && !seen {
		f[field] = message
	}
}

func (f fieldErrors) err() error {
	if len(f) == 0 {
		return nil
	}
	return &Error{Status: http.StatusBadRequest, Code: "validation_failed", Message: "Some fields are invalid.", Fields: f}
}

func normalizeEmail(s string) string { return strings.ToLower(strings.TrimSpace(s)) }

func validEmail(s string) bool {
	addr, err := netmail.ParseAddress(s)
	return err == nil && addr.Address == s && len(s) <= 254
}
