// Package db opens the Postgres pool from DATABASE_URL.
package db

import (
	"database/sql"
	"time"

	_ "github.com/jackc/pgx/v5/stdlib" // registers the "pgx" driver
)

// Open returns a pool for url, or a nil pool when url is empty so the server
// can still boot without a database. sql.Open does not connect; the first
// query or ping does. Don't pass a nil *sql.DB where an interface is
// expected: it becomes a non-nil interface holding a nil pointer.
func Open(url string) (*sql.DB, error) {
	if url == "" {
		return nil, nil
	}
	pool, err := sql.Open("pgx", url)
	if err != nil {
		return nil, err
	}
	// The smallest Render Postgres plan allows few connections.
	pool.SetMaxOpenConns(10)
	pool.SetMaxIdleConns(5)
	pool.SetConnMaxIdleTime(5 * time.Minute)
	return pool, nil
}
