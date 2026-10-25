package db

import (
	"context"
	"database/sql"
	"io/fs"
	"reflect"
	"sort"
	"strings"
	"sync"
	"testing"
	"testing/fstest"

	"github.com/platanus-hack/platanus-hack-26-ve-team-27/server/internal/dbtest"
	"github.com/platanus-hack/platanus-hack-26-ve-team-27/server/migrations"
)

// freshDB returns an empty throwaway database, or skips without
// TEST_DATABASE_URL.
func freshDB(t *testing.T) *sql.DB { return dbtest.Fresh(t) }

// embeddedVersions lists every migration in migrations.FS in apply order, so
// these tests keep passing when a ticket adds the next NNNN_name.sql.
func embeddedVersions(t *testing.T) []string {
	t.Helper()
	names, err := fs.Glob(migrations.FS, "*.sql")
	if err != nil || len(names) == 0 {
		t.Fatalf("embedded migrations: %v (%d files)", err, len(names))
	}
	sort.Strings(names)
	for i, name := range names {
		names[i] = strings.TrimSuffix(name, ".sql")
	}
	return names
}

func TestMigrateFreshThenNoOp(t *testing.T) {
	pool := freshDB(t)
	ctx := context.Background()

	applied, err := Migrate(ctx, pool, migrations.FS)
	if err != nil {
		t.Fatalf("first Migrate: %v", err)
	}
	want := embeddedVersions(t)
	if !reflect.DeepEqual(applied, want) {
		t.Fatalf("first Migrate applied %v, want %v", applied, want)
	}
	for _, table := range []string{"devices", "incidents", "gps_points", "calls", "schema_migrations"} {
		var ok bool
		if err := pool.QueryRow("SELECT to_regclass($1) IS NOT NULL", table).Scan(&ok); err != nil || !ok {
			t.Errorf("table %s missing (err %v)", table, err)
		}
	}
	// 0002_device_keys adds the device key columns (#55).
	for _, column := range []string{"ed25519_public_key", "key_id"} {
		var ok bool
		err := pool.QueryRow(`SELECT EXISTS (SELECT 1 FROM information_schema.columns
			WHERE table_name = 'devices' AND column_name = $1)`, column).Scan(&ok)
		if err != nil || !ok {
			t.Errorf("column devices.%s missing (err %v)", column, err)
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
	if err := pool.QueryRow("SELECT count(*) FROM schema_migrations").Scan(&n); err != nil || n != len(want) {
		t.Fatalf("schema_migrations rows = %d (err %v), want %d", n, err, len(want))
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
	// One boot applies everything, the other finds it done and applies nothing.
	want := embeddedVersions(t)
	first, second := results[0], results[1]
	if len(first) == 0 {
		first, second = second, first
	}
	if !reflect.DeepEqual(first, want) || len(second) != 0 {
		t.Fatalf("applied %v and %v, want %v exactly once", results[0], results[1], want)
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
