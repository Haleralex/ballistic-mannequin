// Package dbtest gives integration tests a migrated database of their own: a fresh Postgres schema per test in
// the database named by TEST_DATABASE_URL, dropped when the test ends. Tests skip when the variable is unset.
package dbtest

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"io"
	"log/slog"
	"os"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/Haleralex/ballistic-mannequin/server/internal/db"
	"github.com/Haleralex/ballistic-mannequin/server/migrations"
)

// URL returns TEST_DATABASE_URL or skips the test.
func URL(t testing.TB) string {
	t.Helper()
	url := os.Getenv("TEST_DATABASE_URL")
	if url == "" {
		t.Skip("TEST_DATABASE_URL is not set; skipping integration test")
	}
	return url
}

// Quiet is a logger that discards everything.
func Quiet() *slog.Logger { return slog.New(slog.NewTextHandler(io.Discard, nil)) }

// New returns a pool whose search_path is a new, empty schema with all migrations applied.
func New(t testing.TB) *pgxpool.Pool {
	pool, _ := NewWithSchema(t, true)
	return pool
}

// NewWithSchema is New that also returns the schema name; migrate=false leaves the schema empty.
func NewWithSchema(t testing.TB, migrate bool) (*pgxpool.Pool, string) {
	t.Helper()
	url := URL(t)
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	admin, err := pgxpool.New(ctx, url)
	if err != nil {
		t.Fatalf("dbtest: connect: %v", err)
	}
	// citext must live in public so that every test schema sees it; tests in other packages may race here.
	for i := 0; i < 5; i++ {
		var have bool
		if err = admin.QueryRow(ctx, "SELECT EXISTS (SELECT 1 FROM pg_extension WHERE extname = 'citext')").Scan(&have); err == nil && have {
			break
		}
		if _, err = admin.Exec(ctx, "CREATE EXTENSION IF NOT EXISTS citext SCHEMA public"); err == nil {
			break
		}
		time.Sleep(100 * time.Millisecond)
	}
	if err != nil {
		admin.Close()
		t.Fatalf("dbtest: citext: %v", err)
	}

	b := make([]byte, 6)
	_, _ = rand.Read(b)
	schema := "t_" + hex.EncodeToString(b)
	if _, err := admin.Exec(ctx, "CREATE SCHEMA "+schema); err != nil {
		admin.Close()
		t.Fatalf("dbtest: create schema: %v", err)
	}
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		if _, err := admin.Exec(ctx, "DROP SCHEMA "+schema+" CASCADE"); err != nil {
			t.Logf("dbtest: drop schema %s: %v", schema, err)
		}
		admin.Close()
	})

	cfg, err := pgxpool.ParseConfig(url)
	if err != nil {
		t.Fatalf("dbtest: %v", err)
	}
	cfg.ConnConfig.RuntimeParams["search_path"] = schema + ",public"
	cfg.MaxConns = 8
	pool, err := db.ConnectConfig(ctx, cfg, Quiet())
	if err != nil {
		t.Fatalf("dbtest: %v", err)
	}
	t.Cleanup(pool.Close) // registered last, runs first: before the schema is dropped
	if migrate {
		if _, err := db.Migrate(ctx, pool, migrations.FS, Quiet()); err != nil {
			t.Fatalf("dbtest: migrate: %v", err)
		}
	}
	return pool, schema
}
