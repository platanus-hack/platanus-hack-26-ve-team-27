package intake

import (
	"context"
	"database/sql"
	"encoding/json"
	"io/fs"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"
	"testing/fstest"

	"github.com/platanus-hack/platanus-hack-26-ve-team-27/server/internal/contract"
	"github.com/platanus-hack/platanus-hack-26-ve-team-27/server/internal/db"
	"github.com/platanus-hack/platanus-hack-26-ve-team-27/server/internal/dbtest"
	"github.com/platanus-hack/platanus-hack-26-ve-team-27/server/migrations"
)

// The duress phrase of device_registration.json.
const samplePhrase = "dile a la tía Carmen que llego tarde"

func TestValidateSetup(t *testing.T) {
	cases := []struct {
		name, user, safe string
		ok               bool
	}{
		{"neither: an older app", "", "", true},
		{"name of 30 characters", strings.Repeat("ñ", 30), "", true},
		{"name of 31 characters", strings.Repeat("ñ", 31), "", false},
		{"unrelated safe phrase", "Mariana", "ya llegué a la casa, todo tranquilo", true},
		{"one shared key word", "", "llego temprano hoy", true},
		{"same phrase", "", samplePhrase, false},
		{"same phrase, other case and accents", "", "Dile a la tia CARMEN que llegó tarde.", false},
		{"close variant", "", "dile a mi tía Carmen que llego tarde", false},
		{"most of its key words", "", "la tía Carmen llego tarde", false},
		{"three of its five key words", "", "la tía Carmen llegó", false},
		{"punctuation only", "", "¡¿...?!", false},
	}
	for _, c := range cases {
		reg := contract.DeviceRegistration{DuressPhrase: samplePhrase, UserName: c.user, SafePhrase: c.safe}
		err := validateSetup(reg)
		if (err == nil) != c.ok {
			t.Errorf("%s: validateSetup = %v, want ok %v", c.name, err, c.ok)
		}
		if err != nil && c.safe != "" && strings.Contains(err.Error(), c.safe) {
			t.Errorf("%s: the error quotes the phrase: %v", c.name, err)
		}
	}
}

// A bad name or a safe phrase too close to the duress phrase is a 422
// before any query, so a lazy pool is enough. A blank name is no name.
func TestBadSetupIs422WithoutDB(t *testing.T) {
	pool, err := sql.Open("pgx", "postgres://invalid.invalid/none")
	if err != nil {
		t.Fatal(err)
	}
	defer pool.Close()
	mux := http.NewServeMux()
	New(pool, LogHandoff{}, LogHandoff{}).Register(mux)

	body := func(fields string) string {
		return `{"device_id":"x","phone_number":"+584141234567","family_numbers":[],"duress_phrase":"` + samplePhrase + `"` + fields + `}`
	}
	cases := map[string]string{
		"name too long":          body(`,"user_name":"` + strings.Repeat("a", 31) + `"`),
		"safe equals duress":     body(`,"safe_phrase":"` + samplePhrase + `"`),
		"safe nearly duress":     body(`,"safe_phrase":"Dile a la tía Carmen que llegó tarde"`),
		"safe without any words": body(`,"safe_phrase":"..."`),
	}
	for name, b := range cases {
		rec := httptest.NewRecorder()
		mux.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/devices", strings.NewReader(b)))
		if rec.Code != http.StatusUnprocessableEntity || !strings.Contains(rec.Body.String(), `"error"`) {
			t.Errorf("%s: status = %d %s, want 422 with an error", name, rec.Code, rec.Body)
		}
	}
}

func (e *env) setupOf(id string) (user, safe sql.NullString) {
	e.t.Helper()
	if err := e.pool.QueryRow(`SELECT user_name, safe_phrase FROM devices WHERE id = $1`, id).Scan(&user, &safe); err != nil {
		e.t.Fatal(err)
	}
	return user, safe
}

func TestDeviceRegistrationKeepsNameAndSafePhrase(t *testing.T) {
	e := newEnv(t) // registered device_registration.json, which carries both
	user, safe := e.setupOf(deviceID)
	if user.String != "Mariana" || safe.String != "ya llegué a la casa, todo tranquilo" {
		t.Fatalf("after the sample: user_name %v, safe_phrase set %v", user, safe.Valid)
	}

	// An older app sends neither: the stored ones stay.
	r := signedReg(deviceID, vectorKey)
	r.DuressPhrase = samplePhrase
	old, _ := json.Marshal(r)
	if rec := e.post("/devices", old); rec.Code != http.StatusNoContent {
		t.Fatalf("register without them = %d %s", rec.Code, rec.Body)
	}
	if u, s := e.setupOf(deviceID); u != user || s != safe {
		t.Fatalf("without them: user_name %v, safe_phrase changed %v", u, s != safe)
	}

	// New values replace them, trimmed.
	r.UserName, r.SafePhrase = "  Mari ", "todo bien por aquí"
	changed, _ := json.Marshal(r)
	if rec := e.post("/devices", changed); rec.Code != http.StatusNoContent {
		t.Fatalf("register with new ones = %d %s", rec.Code, rec.Body)
	}
	if u, s := e.setupOf(deviceID); u.String != "Mari" || s.String != "todo bien por aquí" {
		t.Fatalf("with new ones: user_name %v, safe_phrase %v", u, s)
	}

	// A rejected one changes nothing.
	r.SafePhrase = samplePhrase
	bad, _ := json.Marshal(r)
	if rec := e.post("/devices", bad); rec.Code != http.StatusUnprocessableEntity {
		t.Fatalf("register with the duress phrase as safe = %d %s, want 422", rec.Code, rec.Body)
	}
	if _, s := e.setupOf(deviceID); s.String != "todo bien por aquí" {
		t.Fatal("a 422 changed safe_phrase")
	}
}

// A device registered by an older app has no name or safe phrase: NULL.
func TestDeviceRegistrationWithoutSetupStoresNull(t *testing.T) {
	e := newEnv(t)
	r := contract.DeviceRegistration{DeviceID: "dev-old", PhoneNumber: "+584141234567", DuressPhrase: samplePhrase}
	body, _ := json.Marshal(r)
	if rec := e.post("/devices", body); rec.Code != http.StatusNoContent {
		t.Fatalf("POST /devices = %d %s", rec.Code, rec.Body)
	}
	if u, s := e.setupOf("dev-old"); u.Valid || s.Valid {
		t.Fatalf("user_name %v, safe_phrase set %v; want both NULL", u, s.Valid)
	}
}

// 0005_device_setup on a database that already has devices: the rows stay,
// the new columns are NULL, and a second boot applies nothing.
func TestDeviceSetupMigrationOnExistingData(t *testing.T) {
	pool := dbtest.Fresh(t)
	ctx := context.Background()
	const setup = "0005_device_setup.sql"

	before := fstest.MapFS{}
	names, err := fs.Glob(migrations.FS, "*.sql")
	if err != nil {
		t.Fatal(err)
	}
	for _, name := range names {
		if name >= setup {
			continue
		}
		raw, err := fs.ReadFile(migrations.FS, name)
		if err != nil {
			t.Fatal(err)
		}
		before[name] = &fstest.MapFile{Data: raw}
	}
	if _, err := db.Migrate(ctx, pool, before); err != nil {
		t.Fatalf("migrate up to %s: %v", setup, err)
	}
	if _, err := pool.Exec(`INSERT INTO devices (id, phone_number, family_numbers, duress_phrase) VALUES ('dev-1', '+584141234567', '{+584241112233}', $1)`, samplePhrase); err != nil {
		t.Fatal(err)
	}

	applied, err := db.Migrate(ctx, pool, migrations.FS)
	if err != nil {
		t.Fatalf("migrate: %v", err)
	}
	if !contains(applied, strings.TrimSuffix(setup, ".sql")) {
		t.Fatalf("applied %v, want %s among them", applied, setup)
	}
	var phone, phrase string
	var user, safe, voice sql.NullString
	err = pool.QueryRow(`SELECT phone_number, duress_phrase, user_name, safe_phrase, voice_id FROM devices WHERE id = 'dev-1'`).
		Scan(&phone, &phrase, &user, &safe, &voice)
	if err != nil {
		t.Fatal(err)
	}
	if phone != "+584141234567" || phrase != samplePhrase || user.Valid || safe.Valid || voice.Valid {
		t.Fatalf("device after the migration: %s, phrase kept %v, %v %v %v", phone, phrase == samplePhrase, user, safe.Valid, voice)
	}

	again, err := db.Migrate(ctx, pool, migrations.FS)
	if err != nil || len(again) != 0 {
		t.Fatalf("second boot applied %v (err %v), want nothing", again, err)
	}
}

// On a fresh database the columns exist after the first boot.
func TestDeviceSetupMigrationOnFreshDatabase(t *testing.T) {
	e := newEnv(t)
	var cols []string
	rows, err := e.pool.Query(`SELECT column_name FROM information_schema.columns
		WHERE table_name = 'devices' AND column_name IN ('user_name', 'safe_phrase', 'voice_id') ORDER BY column_name`)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	for rows.Next() {
		var c string
		if err := rows.Scan(&c); err != nil {
			t.Fatal(err)
		}
		cols = append(cols, c)
	}
	if want := []string{"safe_phrase", "user_name", "voice_id"}; !reflect.DeepEqual(cols, want) {
		t.Fatalf("devices columns = %v, want %v", cols, want)
	}
	if again, err := db.Migrate(context.Background(), e.pool, migrations.FS); err != nil || len(again) != 0 {
		t.Fatalf("second boot applied %v (err %v), want nothing", again, err)
	}
}

func contains(list []string, s string) bool {
	for _, v := range list {
		if v == s {
			return true
		}
	}
	return false
}
