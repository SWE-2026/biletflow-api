package mail

import (
	"bytes"
	"context"
	"crypto/rand"
	"crypto/tls"
	"encoding/base64"
	"errors"
	"fmt"
	"mime"
	"mime/multipart"
	"mime/quotedprintable"
	"net"
	"net/mail"
	"net/smtp"
	"net/textproto"
	"strconv"
	"strings"
	"time"
)

type Attachment struct {
	Name        string
	ContentType string
	Data        []byte
}

type SMTPMailer struct {
	Host     string
	Port     int
	Username string
	Password string
	From     string
	Timeout  time.Duration
}

func (m SMTPMailer) Send(ctx context.Context, msg Message) error {
	body, err := buildMessage(m.From, msg)
	if err != nil {
		return err
	}
	c, err := m.connect(ctx)
	if err != nil {
		return err
	}
	defer c.Close()
	if m.Username != "" {
		if err := c.Auth(smtp.PlainAuth("", m.Username, m.Password, m.Host)); err != nil {
			return fmt.Errorf("smtp auth: %w", err)
		}
	}
	if err := c.Mail(m.From); err != nil {
		return fmt.Errorf("smtp mail from: %w", err)
	}
	if err := c.Rcpt(msg.To); err != nil {
		return fmt.Errorf("smtp rcpt: %w", err)
	}
	w, err := c.Data()
	if err != nil {
		return fmt.Errorf("smtp data: %w", err)
	}
	if _, err := w.Write(body); err != nil {
		return fmt.Errorf("smtp write: %w", err)
	}
	if err := w.Close(); err != nil {
		return fmt.Errorf("smtp close data: %w", err)
	}
	return c.Quit()
}

func (m SMTPMailer) connect(ctx context.Context) (*smtp.Client, error) {
	dialer := net.Dialer{Timeout: m.Timeout}
	conn, err := dialer.DialContext(ctx, "tcp", net.JoinHostPort(m.Host, strconv.Itoa(m.Port)))
	if err != nil {
		return nil, fmt.Errorf("smtp dial: %w", err)
	}
	if err := conn.SetDeadline(time.Now().Add(m.Timeout)); err != nil {
		conn.Close()
		return nil, err
	}
	tlsConfig := &tls.Config{ServerName: m.Host, MinVersion: tls.VersionTLS12}
	if m.Port == 465 {
		conn = tls.Client(conn, tlsConfig)
	}
	c, err := smtp.NewClient(conn, m.Host)
	if err != nil {
		conn.Close()
		return nil, fmt.Errorf("smtp handshake: %w", err)
	}
	if m.Port == 465 {
		return c, nil
	}
	if ok, _ := c.Extension("STARTTLS"); !ok {
		c.Close()
		return nil, errors.New("smtp server does not offer STARTTLS")
	}
	if err := c.StartTLS(tlsConfig); err != nil {
		c.Close()
		return nil, fmt.Errorf("smtp starttls: %w", err)
	}
	return c, nil
}

func headerSafe(s string) string { return strings.NewReplacer("\r", " ", "\n", " ").Replace(s) }

func buildMessage(from string, msg Message) ([]byte, error) {
	var buf bytes.Buffer
	mw := multipart.NewWriter(&buf)
	headers := []string{
		"From: " + (&mail.Address{Address: headerSafe(from)}).String(),
		"To: " + (&mail.Address{Address: headerSafe(msg.To)}).String(),
		"Subject: " + mime.QEncoding.Encode("utf-8", headerSafe(msg.Subject)),
		"Date: " + time.Now().Format(time.RFC1123Z),
		"Message-ID: <" + rand.Text() + "@biletflow>",
		"MIME-Version: 1.0",
		"Content-Type: multipart/mixed; boundary=" + mw.Boundary(),
	}
	var out bytes.Buffer
	out.WriteString(strings.Join(headers, "\r\n") + "\r\n\r\n")
	text, err := mw.CreatePart(textproto.MIMEHeader{
		"Content-Type":              {"text/plain; charset=utf-8"},
		"Content-Transfer-Encoding": {"quoted-printable"},
	})
	if err != nil {
		return nil, err
	}
	qp := quotedprintable.NewWriter(text)
	if _, err := qp.Write([]byte(msg.Text)); err != nil {
		return nil, err
	}
	if err := qp.Close(); err != nil {
		return nil, err
	}
	for _, a := range msg.Attachments {
		if err := writeAttachment(mw, a); err != nil {
			return nil, err
		}
	}
	if err := mw.Close(); err != nil {
		return nil, err
	}
	out.Write(buf.Bytes())
	return out.Bytes(), nil
}

func writeAttachment(mw *multipart.Writer, a Attachment) error {
	part, err := mw.CreatePart(textproto.MIMEHeader{
		"Content-Type":              {a.ContentType},
		"Content-Transfer-Encoding": {"base64"},
		"Content-Disposition":       {mime.FormatMediaType("attachment", map[string]string{"filename": a.Name})},
	})
	if err != nil {
		return err
	}
	encoded := base64.StdEncoding.EncodeToString(a.Data)
	for len(encoded) > 76 {
		if _, err := part.Write([]byte(encoded[:76] + "\r\n")); err != nil {
			return err
		}
		encoded = encoded[76:]
	}
	_, err = part.Write([]byte(encoded + "\r\n"))
	return err
}
