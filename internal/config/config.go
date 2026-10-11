// Package config reads the API's settings from environment variables. Every variable read here is
// documented in .env.example.
package config

import (
	"errors"
	"os"
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
}

func (c Config) IsLocal() bool { return c.Env == EnvLocal }

func Load() (Config, error) {
	c := Config{
		DatabaseURL: os.Getenv("DATABASE_URL"),
		Port:        valueOr(os.Getenv("PORT"), "8080"),
		Env:         valueOr(os.Getenv("APP_ENV"), EnvProduction),
		CORSOrigins: splitList(os.Getenv("CORS_ORIGINS")),
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
