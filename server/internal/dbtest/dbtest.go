// Package dbtest gives tests a throwaway Postgres database.
package dbtest

import (
	"database/sql"
	"fmt"
	"net/url"
	"os"
	"testing"
	"time"

	_ "github.com/jackc/pgx/v5/stdlib" // registers the "pgx" driver
)

// Fresh creates an empty database on the server at TEST_DATABASE_URL and
// returns a pool on it, dropped at the end of the test. It skips the test when
// TEST_DATABASE_URL is unset. Never point it at a shared or live database.
func Fresh(t *testing.T) *sql.DB {
	t.Helper()
	admin := os.Getenv("TEST_DATABASE_URL")
	if admin == "" {
		t.Skip("TEST_DATABASE_URL not set")
	}
	adminPool, err := sql.Open("pgx", admin)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { adminPool.Close() })

	name := fmt.Sprintf("mama_test_%d", time.Now().UnixNano())
	if _, err := adminPool.Exec("CREATE DATABASE " + name); err != nil {
		t.Fatalf("create database: %v", err)
	}
	u, err := url.Parse(admin)
	if err != nil {
		t.Fatal(err)
	}
	u.Path = "/" + name
	pool, err := sql.Open("pgx", u.String())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		pool.Close()
		if _, err := adminPool.Exec("DROP DATABASE IF EXISTS " + name + " WITH (FORCE)"); err != nil {
			t.Logf("drop database %s: %v", name, err)
		}
	})
	return pool
}
