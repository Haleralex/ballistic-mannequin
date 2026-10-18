// Command api is the ballistic-mannequin backend: the HTTP API, share pages, the admin dashboard and the static
// frontend in one process. Configuration comes from the environment (see server/README.md).
//
//	api               run the server
//	api -healthcheck  GET /healthz on the local port and exit 0 if it answers 200 (for container health checks)
//	api -migrate      apply migrations and exit
package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/Haleralex/ballistic-mannequin/server/internal/config"
	"github.com/Haleralex/ballistic-mannequin/server/internal/db"
	"github.com/Haleralex/ballistic-mannequin/server/internal/httpapi"
	"github.com/Haleralex/ballistic-mannequin/server/internal/store"
	"github.com/Haleralex/ballistic-mannequin/server/migrations"
)

func main() {
	healthcheck := flag.Bool("healthcheck", false, "check GET /healthz on the local PORT and exit")
	migrateOnly := flag.Bool("migrate", false, "apply migrations and exit")
	flag.Parse()

	cfg, err := config.FromEnv()
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(2)
	}
	if *healthcheck {
		os.Exit(runHealthcheck(cfg))
	}
	log := newLogger(cfg.LogLevel)
	if err := run(cfg, log, *migrateOnly); err != nil {
		log.Error("exit", "err", err)
		os.Exit(1)
	}
}

func newLogger(level string) *slog.Logger {
	var l slog.Level
	if err := l.UnmarshalText([]byte(level)); err != nil {
		l = slog.LevelInfo
	}
	return slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{Level: l}))
}

func run(cfg config.Config, log *slog.Logger, migrateOnly bool) error {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	cctx, cancel := context.WithTimeout(ctx, 60*time.Second)
	pool, err := db.Connect(cctx, cfg.DatabaseURL, log)
	cancel()
	if err != nil {
		return err
	}
	defer pool.Close()

	mctx, cancel := context.WithTimeout(ctx, 5*time.Minute)
	applied, err := db.Migrate(mctx, pool, migrations.FS, log)
	cancel()
	if err != nil {
		return err
	}
	log.Info("migrations up to date", "applied_now", len(applied))
	if migrateOnly {
		return nil
	}

	srv := httpapi.New(cfg, store.New(pool), log, httpapi.DefaultLimits)
	jctx, jcancel := context.WithCancel(context.Background())
	defer jcancel()
	go srv.Janitor(jctx)

	hs := &http.Server{
		Addr:              ":" + cfg.Port,
		Handler:           srv.Handler(),
		ReadHeaderTimeout: 10 * time.Second,
		ReadTimeout:       30 * time.Second,
		WriteTimeout:      60 * time.Second, // static files extend their own deadline
		IdleTimeout:       120 * time.Second,
		MaxHeaderBytes:    64 << 10,
		ErrorLog:          slog.NewLogLogger(log.Handler(), slog.LevelWarn),
	}
	errc := make(chan error, 1)
	go func() {
		log.Info("listening", "addr", hs.Addr, "build", cfg.Build, "static", cfg.StaticDir,
			"admin", cfg.AdminEnabled(), "trust_proxy", cfg.TrustProxy, "origins", cfg.AllowedOrigins)
		errc <- hs.ListenAndServe()
	}()

	select {
	case err := <-errc:
		if !errors.Is(err, http.ErrServerClosed) {
			return err
		}
	case <-ctx.Done():
		log.Info("shutting down")
	}
	sctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	if err := hs.Shutdown(sctx); err != nil {
		log.Warn("shutdown", "err", err)
	}
	srv.Wait() // pending view counts
	log.Info("stopped")
	return nil
}

func runHealthcheck(cfg config.Config) int {
	c := &http.Client{Timeout: 3 * time.Second}
	resp, err := c.Get("http://127.0.0.1:" + cfg.Port + "/healthz")
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		fmt.Fprintln(os.Stderr, "healthz:", resp.Status)
		return 1
	}
	return 0
}
