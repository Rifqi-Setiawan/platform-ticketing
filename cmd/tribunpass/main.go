// Command tribunpass starts the booking API and its PostgreSQL expiry worker.
package main

import (
	"context"
	"crypto/rand"
	"database/sql"
	"errors"
	"fmt"
	_ "github.com/jackc/pgx/v5/stdlib"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"strconv"
	"syscall"
	"time"
	"tribunpass/internal/expiry"
	"tribunpass/internal/httpapi"
	"tribunpass/internal/postgres"
	"tribunpass/migrations"
)

func main() {
	logger := slog.New(slog.NewJSONHandler(os.Stdout, nil))
	if err := run(logger); err != nil {
		logger.Error("server stopped", "error", err)
		os.Exit(1)
	}
}

func env(key, fallback string) string {
	if value := os.Getenv(key); value != "" {
		return value
	}
	return fallback
}

func run(log *slog.Logger) error {
	root, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	dsn := os.Getenv("DATABASE_URL")
	if dsn == "" {
		return errors.New("DATABASE_URL is required")
	}
	pool, err := strconv.Atoi(env("DB_MAX_OPEN", "10"))
	if err != nil || pool < 1 || pool > 200 {
		return errors.New("DB_MAX_OPEN must be between 1 and 200")
	}
	secure, err := strconv.ParseBool(env("SECURE_COOKIES", "false"))
	if err != nil {
		return fmt.Errorf("SECURE_COOKIES: %w", err)
	}
	secret := []byte(os.Getenv("SIMULATOR_SECRET"))
	if len(secret) == 0 {
		secret = make([]byte, 32)
		if _, err := rand.Read(secret); err != nil {
			return err
		}
	} else if len(secret) < 32 {
		return errors.New("SIMULATOR_SECRET must be at least 32 bytes")
	}
	db, err := sql.Open("pgx", dsn)
	if err != nil {
		return err
	}
	defer db.Close()
	db.SetMaxOpenConns(pool)
	db.SetMaxIdleConns(pool)
	db.SetConnMaxIdleTime(5 * time.Minute)
	startup, cancel := context.WithTimeout(root, 30*time.Second)
	err = migrations.Apply(startup, db)
	cancel()
	if err != nil {
		return fmt.Errorf("apply migrations: %w", err)
	}
	store := postgres.New(db)
	observed := newObservedStore(store)
	mux := http.NewServeMux()
	mux.HandleFunc("GET /metrics", observed.metrics)
	port := env("PORT", "8080")
	mux.Handle("/", httpapi.New(observed, httpapi.Config{Secret: secret, SecureCookies: secure, Logger: log, Ready: db.PingContext, CallbackURL: env("CALLBACK_URL", "http://127.0.0.1:"+port+"/v1/payment-callbacks/simulator")}))
	srv := &http.Server{Addr: env("BIND_HOST", "") + ":" + port, Handler: mux, ReadHeaderTimeout: 5 * time.Second, ReadTimeout: 12 * time.Second, WriteTimeout: 15 * time.Second, IdleTimeout: 60 * time.Second}
	workerCtx, stopWorker := context.WithCancel(root)
	defer stopWorker()
	workerDone := make(chan struct{})
	go func() { defer close(workerDone); expiry.Run(workerCtx, store, log) }()
	serverErr := make(chan error, 1)
	go func() { serverErr <- srv.ListenAndServe() }()
	log.Info("booking API started", "addr", srv.Addr, "db_max_open", pool)
	select {
	case <-root.Done():
	case err = <-serverErr:
	}
	stopWorker()
	shutdown, finish := context.WithTimeout(context.Background(), 10*time.Second)
	defer finish()
	shutdownErr := srv.Shutdown(shutdown)
	if shutdownErr != nil {
		_ = srv.Close()
	}
	<-workerDone
	if err != nil && !errors.Is(err, http.ErrServerClosed) {
		return err
	}
	return shutdownErr
}
