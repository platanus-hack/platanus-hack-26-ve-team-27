package db

import (
	"context"
	"database/sql"
	"fmt"
	"io/fs"
	"sort"
	"strings"
)

// migrateLockKey is the pg_advisory_lock key that serializes concurrent
// boots (two Render instances, or a deploy overlapping the old one).
const migrateLockKey = 7_270_009

// Migrate applies every *.sql file in fsys that schema_migrations does not
// list yet, in name order, each in its own transaction together with its
// schema_migrations row. Running it again applies nothing. It returns the
// names it applied.
func Migrate(ctx context.Context, pool *sql.DB, fsys fs.FS) ([]string, error) {
	names, err := fs.Glob(fsys, "*.sql")
	if err != nil {
		return nil, err
	}
	sort.Strings(names)

	// The advisory lock belongs to a session, so hold one connection.
	conn, err := pool.Conn(ctx)
	if err != nil {
		return nil, fmt.Errorf("migrate: connect: %w", err)
	}
	defer conn.Close()
	if _, err := conn.ExecContext(ctx, "SELECT pg_advisory_lock($1)", migrateLockKey); err != nil {
		return nil, fmt.Errorf("migrate: lock: %w", err)
	}
	// Unlock with a fresh context so a cancelled ctx still releases it.
	defer conn.ExecContext(context.Background(), "SELECT pg_advisory_unlock($1)", migrateLockKey) //nolint:errcheck

	if _, err := conn.ExecContext(ctx, `CREATE TABLE IF NOT EXISTS schema_migrations (
		version    TEXT PRIMARY KEY,
		applied_at TIMESTAMPTZ NOT NULL DEFAULT now()
	)`); err != nil {
		return nil, fmt.Errorf("migrate: create schema_migrations: %w", err)
	}

	done := map[string]bool{}
	rows, err := conn.QueryContext(ctx, "SELECT version FROM schema_migrations")
	if err != nil {
		return nil, fmt.Errorf("migrate: read schema_migrations: %w", err)
	}
	for rows.Next() {
		var v string
		if err := rows.Scan(&v); err != nil {
			rows.Close()
			return nil, err
		}
		done[v] = true
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return nil, err
	}

	var applied []string
	for _, name := range names {
		version := strings.TrimSuffix(name, ".sql")
		if done[version] {
			continue
		}
		body, err := fs.ReadFile(fsys, name)
		if err != nil {
			return applied, err
		}
		if err := applyOne(ctx, conn, version, string(body)); err != nil {
			return applied, fmt.Errorf("migrate: %s: %w", name, err)
		}
		applied = append(applied, version)
	}
	return applied, nil
}

func applyOne(ctx context.Context, conn *sql.Conn, version, body string) error {
	tx, err := conn.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback() //nolint:errcheck // no-op after Commit
	// No arguments, so pgx sends the file with the simple protocol, which
	// allows several statements in one Exec.
	if _, err := tx.ExecContext(ctx, body); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, "INSERT INTO schema_migrations (version) VALUES ($1)", version); err != nil {
		return err
	}
	return tx.Commit()
}
