package httpapi

import (
	"io"
	"mime/multipart"
	"net/http"
	"path/filepath"
	"slices"
	"strings"
	"unicode"

	"github.com/SWE-2026/biletflow-api/internal/store"
	"github.com/labstack/echo/v4"
)

const (
	maxAttachments      = 3
	maxAttachmentBytes  = 5 << 20
	attachmentBodyLimit = "16M"
	supportMessagePath  = "/api/v1/support/cases/:id/messages"
)

var attachmentTypes = []string{"image/png", "image/jpeg", "image/gif", "image/webp", "application/pdf", "text/plain"}

func isMultipart(c echo.Context) bool {
	return strings.HasPrefix(c.Request().Header.Get(echo.HeaderContentType), echo.MIMEMultipartForm)
}

func largeBodyRoute(c echo.Context) bool {
	return c.Path() == supportMessagePath && isMultipart(c)
}

func cleanFileName(name string) string {
	name = strings.Map(func(r rune) rune {
		if unicode.IsControl(r) || strings.ContainsRune(`"\/:*?<>|`, r) {
			return '_'
		}
		return r
	}, filepath.Base(name))
	name = strings.TrimSpace(name)
	if runes := []rune(name); len(runes) > 120 {
		name = string(runes[:120])
	}
	if name == "" || name == "." {
		return "attachment"
	}
	return name
}

func readAttachment(header *multipart.FileHeader) (store.SupportFile, bool, error) {
	if header.Size <= 0 || header.Size > maxAttachmentBytes {
		return store.SupportFile{}, false, nil
	}
	f, err := header.Open()
	if err != nil {
		return store.SupportFile{}, false, err
	}
	defer f.Close()
	data, err := io.ReadAll(io.LimitReader(f, maxAttachmentBytes+1))
	if err != nil {
		return store.SupportFile{}, false, err
	}
	contentType, _, _ := strings.Cut(http.DetectContentType(data), ";")
	ok := len(data) > 0 && len(data) <= maxAttachmentBytes && slices.Contains(attachmentTypes, contentType)
	return store.SupportFile{Name: cleanFileName(header.Filename), ContentType: contentType, Data: data}, ok, nil
}

func decodeSupportMultipart(c echo.Context) (string, []store.SupportFile, error) {
	form, err := c.MultipartForm()
	if err != nil {
		return "", nil, errInvalidJSON
	}
	body := ""
	if values := form.Value["body"]; len(values) > 0 {
		body = strings.TrimSpace(values[0])
	}
	headers := form.File["files"]
	fe := fieldErrors{}
	fe.check(len(headers) <= maxAttachments, "files", "Attach up to 3 files.")
	fe.check(len(body) <= 5000, "body", "Enter a message (up to 5000 characters).")
	fe.check(len(body) > 0 || len(headers) > 0, "body", "Enter a message or attach a file.")
	files := make([]store.SupportFile, 0, len(headers))
	for _, header := range headers[:min(len(headers), maxAttachments)] {
		file, ok, err := readAttachment(header)
		if err != nil {
			return "", nil, err
		}
		fe.check(ok, "files", "Attach PNG, JPEG, GIF, WebP, PDF or plain text files up to 5 MB each.")
		files = append(files, file)
	}
	return body, files, fe.err()
}

func asciiFileName(name string) string {
	return strings.Map(func(r rune) rune {
		if r < 128 && (unicode.IsLetter(r) || unicode.IsDigit(r) || strings.ContainsRune("._-", r)) {
			return r
		}
		return '_'
	}, name)
}

func (s *Server) supportAttachment(c echo.Context) error {
	id, err := pathID(c, "id")
	if err != nil {
		return err
	}
	attachmentID, err := pathID(c, "attachmentId")
	if err != nil {
		return err
	}
	a, data, err := s.db.SupportAttachmentFile(c.Request().Context(), supportViewer(c), id, attachmentID)
	if err != nil {
		return err
	}
	c.Response().Header().Set("X-Content-Type-Options", "nosniff")
	return sendFile(c, a.ContentType, asciiFileName(a.FileName), data)
}
