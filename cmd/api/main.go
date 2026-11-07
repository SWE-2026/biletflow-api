package main

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"sync"
	"syscall"
	"time"
	_ "time/tzdata" // event time zones must resolve in minimal containers too

	"github.com/SWE-2026/biletflow-api/internal/auth"
	"github.com/SWE-2026/biletflow-api/internal/config"
	"github.com/SWE-2026/biletflow-api/internal/httpapi"
	"github.com/SWE-2026/biletflow-api/internal/mail"
	"github.com/SWE-2026/biletflow-api/internal/notify"
	"github.com/SWE-2026/biletflow-api/internal/store"
)

func main() {
	logger := slog.New(slog.NewJSONHandler(os.Stdout, nil))
	if err := run(logger); err != nil {
		logger.Error("api stopped", "error", err)
		os.Exit(1)
	}
}

func newMailer(cfg config.Config, logger *slog.Logger) notify.Mailer {
	if cfg.SMTP.Host == "" {
		logger.Info("no SMTP_HOST configured: emails are logged, not sent")
		return mail.LogMailer{Logger: logger}
	}
	return mail.SMTPMailer{Host: cfg.SMTP.Host, Port: cfg.SMTP.Port, Username: cfg.SMTP.Username,
		Password: cfg.SMTP.Password, From: cfg.SMTP.From, Timeout: 15 * time.Second}
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
	promoted, err := db.PromoteAdmins(ctx, cfg.AdminEmails)
	if err != nil {
		return err
	}
	logger.Info("admin accounts checked", "configured", len(cfg.AdminEmails), "promoted", promoted)

	mailer := newMailer(cfg, logger)
	codes := auth.NewCodes(db, mailer)
	server := httpapi.New(db, codes, httpapi.Options{
		SessionTTL:  7 * 24 * time.Hour,
		ExposeCodes: cfg.IsLocal(),
		CORSOrigins: cfg.CORSOrigins,
		Logger:      logger,
		WebURL:      cfg.WebURL,
		TrustProxy:  cfg.TrustProxy,
	})

	dispatchCtx, stopDispatch := context.WithCancel(context.Background())
	var workers sync.WaitGroup
	workers.Add(1)
	go func() {
		defer workers.Done()
		notify.Dispatcher{DB: db, Mailer: mailer, WebURL: cfg.WebURL, Logger: logger, Interval: 5 * time.Second}.Run(dispatchCtx)
	}()
	defer workers.Wait()
	defer stopDispatch()

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
