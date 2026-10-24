package db

import (
	"context"
	"database/sql"
	"reflect"
	"sync"
	"testing"
	"testing/fstest"

	"github.com/platanus-hack/platanus-hack-26-ve-team-27/server/internal/dbtest"
	"github.com/platanus-hack/platanus-hack-26-ve-team-27/server/migrations"
)

// freshDB returns an empty throwaway database, or skips without
// TEST_DATABASE_URL.
func freshDB(t *testing.T) *sql.DB { return dbtest.Fresh(t) }

func TestMigrateFreshThenNoOp(t *testing.T) {
	pool := freshDB(t)
	ctx := context.Background()

	applied, err := Migrate(ctx, pool, migrations.FS)
	if err != nil {
		t.Fatalf("first Migrate: %v", err)
	}
	if want := []string{"0001_init"}; !reflect.DeepEqual(applied, want) {
		t.Fatalf("first Migrate applied %v, want %v", applied, want)
	}
	for _, table := range []string{"devices", "incidents", "gps_points", "calls", "schema_migrations"} {
		var ok bool
		if err := pool.QueryRow("SELECT to_regclass($1) IS NOT NULL", table).Scan(&ok); err != nil || !ok {
			t.Errorf("table %s missing (err %v)", table, err)
		}
	}

	applied, err = Migrate(ctx, pool, migrations.FS)
	if err != nil {
		t.Fatalf("second Migrate: %v", err)
	}
	if len(applied) != 0 {
		t.Fatalf("second Migrate applied %v, want nothing", applied)
	}
	var n int
	if err := pool.QueryRow("SELECT count(*) FROM schema_migrations").Scan(&n); err != nil || n != 1 {
		t.Fatalf("schema_migrations rows = %d (err %v), want 1", n, err)
	}
}

// Two boots at once: the advisory lock makes one apply and the other skip.
func TestMigrateConcurrentBoots(t *testing.T) {
	pool := freshDB(t)
	var wg sync.WaitGroup
	results := make([][]string, 2)
	errs := make([]error, 2)
	for i := range 2 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			results[i], errs[i] = Migrate(context.Background(), pool, migrations.FS)
		}()
	}
	wg.Wait()
	for i, err := range errs {
		if err != nil {
			t.Fatalf("boot %d: %v", i, err)
		}
	}
	if total := len(results[0]) + len(results[1]); total != 1 {
		t.Fatalf("applied %v and %v, want 0001_init exactly once", results[0], results[1])
	}
}

// A failing file rolls back with its schema_migrations row, and the next
// boot retries it.
func TestMigrateFailureRollsBack(t *testing.T) {
	pool := freshDB(t)
	ctx := context.Background()
	bad := fstest.MapFS{
		"0001_ok.sql":  {Data: []byte("CREATE TABLE a (id INT);")},
		"0002_bad.sql": {Data: []byte("CREATE TABLE b (id INT); SELECT * FROM missing_table;")},
	}
	applied, err := Migrate(ctx, pool, bad)
	if err == nil {
		t.Fatal("Migrate with a bad file: want an error")
	}
	if want := []string{"0001_ok"}; !reflect.DeepEqual(applied, want) {
		t.Fatalf("applied %v, want %v", applied, want)
	}
	var b sql.NullString
	if err := pool.QueryRow("SELECT to_regclass('b')::text").Scan(&b); err != nil || b.Valid {
		t.Fatalf("table b exists after a failed migration (err %v)", err)
	}

	bad["0002_bad.sql"] = &fstest.MapFile{Data: []byte("CREATE TABLE b (id INT);")}
	applied, err = Migrate(ctx, pool, bad)
	if err != nil || !reflect.DeepEqual(applied, []string{"0002_bad"}) {
		t.Fatalf("retry applied %v (err %v), want [0002_bad]", applied, err)
	}
}
