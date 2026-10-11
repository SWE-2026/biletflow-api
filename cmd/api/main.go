package main

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"
	_ "time/tzdata" // event time zones must resolve in minimal containers too

	"github.com/SWE-2026/biletflow-api/internal/auth"
	"github.com/SWE-2026/biletflow-api/internal/config"
	"github.com/SWE-2026/biletflow-api/internal/httpapi"
	"github.com/SWE-2026/biletflow-api/internal/mail"
	"github.com/SWE-2026/biletflow-api/internal/store"
)

func main() {
	logger := slog.New(slog.NewJSONHandler(os.Stdout, nil))
	if err := run(logger); err != nil {
		logger.Error("api stopped", "error", err)
		os.Exit(1)
	}
}

func run(logger *slog.Logger) error {
	cfg, err := config.Load()
	if err != nil {
		return err
	}
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	db, err := store.NewPostgres(ctx, cfg.DatabaseURL)
	if err != nil {
		return err
	}
	defer db.Close()
	if err := db.Migrate(ctx); err != nil {
		return err
	}

	codes := auth.NewCodes(db, mail.LogMailer{Logger: logger})
	server := httpapi.New(db, codes, httpapi.Options{
		SessionTTL:  7 * 24 * time.Hour,
		ExposeCodes: cfg.IsLocal(),
		CORSOrigins: cfg.CORSOrigins,
		Logger:      logger,
	})

	errc := make(chan error, 1)
	go func() { errc <- server.Start(":" + cfg.Port) }()
	logger.Info("api listening", "port", cfg.Port, "env", cfg.Env)

	select {
	case err := <-errc:
		if !errors.Is(err, http.ErrServerClosed) {
			return err
		}
	case <-ctx.Done():
	}
	shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	return server.Shutdown(shutdownCtx)
}
