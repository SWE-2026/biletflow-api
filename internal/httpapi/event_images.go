package httpapi

import (
	"crypto/rand"
	"errors"
	"fmt"
	"io"
	"net/http"
	"regexp"
	"strings"

	"github.com/SWE-2026/biletflow-api/internal/storage"
	"github.com/labstack/echo/v4"
)

const (
	maxEventImageBytes   = 8 << 20
	eventImageBodyLimit  = "9M"
	eventImageUploadPath = "/api/v1/organizer/event-images"
)

var (
	eventImageExtensions  = map[string]string{"image/png": "png", "image/jpeg": "jpg", "image/gif": "gif", "image/webp": "webp"}
	eventImageNamePattern = regexp.MustCompile(`^[A-Za-z0-9]{16,64}\.(png|jpg|gif|webp)$`)
)

func eventImageKey(organizerID, name string) string {
	return "event-images/" + organizerID + "/" + name
}

// uploadEventImage stores one photo (multipart field "file") and returns a public URL
// that can be saved as an event's imageUrl or one of its imageUrls.
func (s *Server) uploadEventImage(c echo.Context) error {
	header, err := c.FormFile("file")
	if err != nil {
		return fieldErrors{"file": "Choose an image to upload."}.err()
	}
	invalid := fieldErrors{"file": "Upload a PNG, JPEG, GIF or WebP image up to 8 MB."}.err()
	if header.Size <= 0 || header.Size > maxEventImageBytes {
		return invalid
	}
	f, err := header.Open()
	if err != nil {
		return err
	}
	defer f.Close()
	data, err := io.ReadAll(io.LimitReader(f, maxEventImageBytes+1))
	if err != nil {
		return err
	}
	contentType, _, _ := strings.Cut(http.DetectContentType(data), ";")
	ext, ok := eventImageExtensions[contentType]
	if !ok || len(data) == 0 || len(data) > maxEventImageBytes {
		return invalid
	}
	orgID := organizerID(c)
	name := rand.Text() + "." + ext
	if err := s.opts.Files.Put(c.Request().Context(), eventImageKey(orgID, name), contentType, data); err != nil {
		return fmt.Errorf("upload event image: %w", err)
	}
	url := c.Scheme() + "://" + c.Request().Host + "/api/v1/event-images/" + orgID + "/" + name
	return c.JSON(http.StatusCreated, map[string]string{"url": url})
}

// eventImage serves an uploaded event photo publicly; names are random and unguessable.
func (s *Server) eventImage(c echo.Context) error {
	orgID, name := c.Param("organizerId"), c.Param("name")
	if !eventIDPattern.MatchString(orgID) || !eventImageNamePattern.MatchString(name) {
		return errNotFound
	}
	data, err := s.opts.Files.Get(c.Request().Context(), eventImageKey(orgID, name))
	if errors.Is(err, storage.ErrNotFound) {
		return errNotFound
	}
	if err != nil {
		return fmt.Errorf("read event image: %w", err)
	}
	contentType, _, _ := strings.Cut(http.DetectContentType(data), ";")
	if _, ok := eventImageExtensions[contentType]; !ok {
		return errNotFound
	}
	h := c.Response().Header()
	h.Set("X-Content-Type-Options", "nosniff")
	h.Set(echo.HeaderCacheControl, "public, max-age=31536000, immutable")
	return c.Blob(http.StatusOK, contentType, data)
}
