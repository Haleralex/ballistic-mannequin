// Package db connects to Postgres and applies the embedded migrations.
package db

import (
	"context"
	"fmt"
	"io/fs"
	"log/slog"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// MigrationLockID is the Postgres advisory lock held while migrating, so that several instances starting at once
// apply each migration exactly once.
const MigrationLockID int64 = 0x6d715f6d69677261 // "mq_migra"

// Connect opens a pool and waits for the database to answer (it may still be starting in docker compose),
// retrying until ctx is done.
func Connect(ctx context.Context, url string, log *slog.Logger) (*pgxpool.Pool, error) {
	cfg, err := pgxpool.ParseConfig(url)
	if err != nil {
		return nil, fmt.Errorf("db: parse DATABASE_URL: %w", err)
	}
	return ConnectConfig(ctx, cfg, log)
}

// ConnectConfig is Connect with a parsed configuration.
func ConnectConfig(ctx context.Context, cfg *pgxpool.Config, log *slog.Logger) (*pgxpool.Pool, error) {
	if cfg.MaxConns < 4 {
		cfg.MaxConns = 10
	}
	pool, err := pgxpool.NewWithConfig(ctx, cfg)
	if err != nil {
		return nil, fmt.Errorf("db: %w", err)
	}
	delay := 250 * time.Millisecond
	for {
		pctx, cancel := context.WithTimeout(ctx, 3*time.Second)
		err = pool.Ping(pctx)
		cancel()
		if err == nil {
			return pool, nil
		}
		log.Warn("database not ready, retrying", "err", err, "in", delay)
		select {
		case <-ctx.Done():
			pool.Close()
			return nil, fmt.Errorf("db: database did not answer: %w", err)
		case <-time.After(delay):
		}
		if delay < 4*time.Second {
			delay *= 2
		}
	}
}

// Migration is one SQL file.
type Migration struct {
	Version int
	Name    string
	SQL     string
}

// Load reads NNN_name.sql files from fsys, sorted by version.
func Load(fsys fs.FS) ([]Migration, error) {
	entries, err := fs.ReadDir(fsys, ".")
	if err != nil {
		return nil, err
	}
	var out []Migration
	seen := map[int]string{}
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".sql") {
			continue
		}
		num, _, ok := strings.Cut(e.Name(), "_")
		v, err := strconv.Atoi(num)
		if !ok || err != nil || v <= 0 {
			return nil, fmt.Errorf("db: migration %q must be named NNN_description.sql", e.Name())
		}
		if prev, dup := seen[v]; dup {
			return nil, fmt.Errorf("db: migrations %q and %q share version %d", prev, e.Name(), v)
		}
		seen[v] = e.Name()
		b, err := fs.ReadFile(fsys, e.Name())
		if err != nil {
			return nil, err
		}
		out = append(out, Migration{Version: v, Name: e.Name(), SQL: string(b)})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Version < out[j].Version })
	return out, nil
}

// Migrate applies the migrations of fsys that are not yet in schema_migrations, each in its own transaction,
// under an advisory lock. It returns the names of the migrations it applied.
func Migrate(ctx context.Context, pool *pgxpool.Pool, fsys fs.FS, log *slog.Logger) ([]string, error) {
	migs, err := Load(fsys)
	if err != nil {
		return nil, err
	}
	conn, err := pool.Acquire(ctx)
	if err != nil {
		return nil, fmt.Errorf("db: acquire: %w", err)
	}
	defer conn.Release()
	if _, err := conn.Exec(ctx, "SELECT pg_advisory_lock($1)", MigrationLockID); err != nil {
		return nil, fmt.Errorf("db: advisory lock: %w", err)
	}
	defer func() {
		// Unlock even if ctx was cancelled; a broken connection releases the lock on its own.
		uctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if _, err := conn.Exec(uctx, "SELECT pg_advisory_unlock($1)", MigrationLockID); err != nil {
			log.Warn("advisory unlock failed", "err", err)
		}
	}()

	if _, err := conn.Exec(ctx, `CREATE TABLE IF NOT EXISTS schema_migrations (
		version    int PRIMARY KEY,
		name       text NOT NULL,
		applied_at timestamptz NOT NULL DEFAULT now()
	)`); err != nil {
		return nil, fmt.Errorf("db: create schema_migrations: %w", err)
	}
	applied := map[int]string{}
	rows, err := conn.Query(ctx, "SELECT version, name FROM schema_migrations")
	if err != nil {
		return nil, fmt.Errorf("db: read schema_migrations: %w", err)
	}
	for rows.Next() {
		var v int
		var n string
		if err := rows.Scan(&v, &n); err != nil {
			rows.Close()
			return nil, err
		}
		applied[v] = n
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return nil, err
	}

	known := map[int]bool{}
	var done []string
	for _, m := range migs {
		known[m.Version] = true
		if name, ok := applied[m.Version]; ok {
			if name != m.Name {
				log.Warn("migration applied under another name", "version", m.Version, "applied", name, "file", m.Name)
			}
			continue
		}
		if err := apply(ctx, conn.Conn(), m); err != nil {
			return done, err
		}
		log.Info("migration applied", "name", m.Name)
		done = append(done, m.Name)
	}
	for v, n := range applied {
		if !known[v] {
			log.Warn("database has a migration this binary does not know (newer build?)", "version", v, "name", n)
		}
	}
	return done, nil
}

func apply(ctx context.Context, conn *pgx.Conn, m Migration) (err error) {
	tx, err := conn.Begin(ctx)
	if err != nil {
		return fmt.Errorf("db: begin %s: %w", m.Name, err)
	}
	defer func() {
		if err != nil {
			_ = tx.Rollback(context.Background())
		}
	}()
	// No arguments: pgx uses the simple protocol, which allows several statements in one string.
	if _, err = tx.Exec(ctx, m.SQL); err != nil {
		return fmt.Errorf("db: migration %s: %w", m.Name, err)
	}
	if _, err = tx.Exec(ctx, "INSERT INTO schema_migrations (version, name) VALUES ($1, $2)", m.Version, m.Name); err != nil {
		return fmt.Errorf("db: record %s: %w", m.Name, err)
	}
	if err = tx.Commit(ctx); err != nil {
		return fmt.Errorf("db: commit %s: %w", m.Name, err)
	}
	return nil
}
