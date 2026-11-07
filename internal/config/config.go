// Package config reads the API's settings from environment variables. Every variable read here is
// documented in .env.example.
package config

import (
	"errors"
	"os"
	"strconv"
	"strings"
)

const (
	EnvLocal      = "local"
	EnvProduction = "production"
)

type Config struct {
	DatabaseURL string
	Port        string
	// Env is "local" or "production". Local mode returns one-time codes in API responses so the
	// demo works without an email provider.
	Env         string
	CORSOrigins []string
	AdminEmails []string
	WebURL      string
	TrustProxy  bool
	SMTP        SMTP
}

type SMTP struct {
	Host     string
	Port     int
	Username string
	Password string
	From     string
}

func (c Config) IsLocal() bool { return c.Env == EnvLocal }

func Load() (Config, error) {
	c := Config{
		DatabaseURL: os.Getenv("DATABASE_URL"),
		Port:        valueOr(os.Getenv("PORT"), "8080"),
		Env:         valueOr(os.Getenv("APP_ENV"), EnvProduction),
		CORSOrigins: splitList(os.Getenv("CORS_ORIGINS")),
		AdminEmails: splitList(strings.ToLower(os.Getenv("ADMIN_EMAILS"))),
		WebURL:      strings.TrimRight(valueOr(os.Getenv("PUBLIC_WEB_URL"), "http://localhost:5173"), "/"),
		TrustProxy:  os.Getenv("TRUST_PROXY") == "true",
		SMTP: SMTP{
			Host:     os.Getenv("SMTP_HOST"),
			Username: os.Getenv("SMTP_USERNAME"),
			Password: os.Getenv("SMTP_PASSWORD"),
			From:     os.Getenv("MAIL_FROM"),
		},
	}
	port, err := strconv.Atoi(valueOr(os.Getenv("SMTP_PORT"), "587"))
	if err != nil || port <= 0 || port > 65535 {
		return c, errors.New("SMTP_PORT must be a port number")
	}
	c.SMTP.Port = port
	if c.SMTP.Host != "" && c.SMTP.From == "" {
		return c, errors.New("MAIL_FROM is required when SMTP_HOST is set")
	}
	if c.DatabaseURL == "" {
		return c, errors.New("DATABASE_URL is required")
	}
	if c.Env != EnvLocal && c.Env != EnvProduction {
		return c, errors.New(`APP_ENV must be "local" or "production"`)
	}
	return c, nil
}

func valueOr(v, fallback string) string {
	if v == "" {
		return fallback
	}
	return v
}

func splitList(v string) []string {
	var out []string
	for item := range strings.SplitSeq(v, ",") {
		if item = strings.TrimSpace(item); item != "" {
			out = append(out, item)
		}
	}
	return out
}
