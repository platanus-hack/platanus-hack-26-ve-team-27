package db

import (
	"context"
	"database/sql"
	"errors"
	"testing"

	"github.com/platanus-hack/platanus-hack-26-ve-team-27/server/internal/contract"
	"github.com/platanus-hack/platanus-hack-26-ve-team-27/server/migrations"
)

func TestSaveVerdict(t *testing.T) {
	pool := freshDB(t)
	ctx := context.Background()
	if _, err := Migrate(ctx, pool, migrations.FS); err != nil {
		t.Fatal(err)
	}
	const incident = "9b2e4f1d-3c5a-4e7b-8d6f-1a2b3c4d5e6f"
	for _, q := range []string{
		`INSERT INTO devices (id, phone_number, duress_phrase) VALUES ('dev-1', '+584141234567', 'dile a la tía Carmen que llego tarde')`,
		`INSERT INTO incidents (id, device_id, trigger, branch, triggered_at) VALUES ('` + incident + `', 'dev-1', 'widget', 'call', now())`,
		`INSERT INTO calls (incident_id, conversation_id) VALUES ('` + incident + `', 'conv_1')`,
	} {
		if _, err := pool.ExecContext(ctx, q); err != nil {
			t.Fatalf("seed: %v", err)
		}
	}

	read := func() (verdict, reason sql.NullString, at sql.NullTime) {
		t.Helper()
		if err := pool.QueryRowContext(ctx,
			`SELECT verdict, verdict_reason, verdict_at FROM calls WHERE conversation_id = 'conv_1'`,
		).Scan(&verdict, &reason, &at); err != nil {
			t.Fatal(err)
		}
		return
	}

	if err := SaveVerdict(ctx, pool, "conv_1", contract.VerdictUnclear, "first"); err != nil {
		t.Fatalf("SaveVerdict: %v", err)
	}
	// A repeated webhook overwrites the verdict.
	if err := SaveVerdict(ctx, pool, "conv_1", contract.VerdictDuress, "the user said the duress phrase"); err != nil {
		t.Fatalf("SaveVerdict again: %v", err)
	}
	verdict, reason, at := read()
	if verdict.String != "duress" || reason.String != "the user said the duress phrase" || !at.Valid {
		t.Fatalf("stored verdict=%v reason=%v at=%v", verdict, reason, at)
	}

	if err := SaveVerdict(ctx, pool, "conv_missing", contract.VerdictDuress, "x"); !errors.Is(err, ErrCallNotFound) {
		t.Fatalf("unknown conversation: got %v, want ErrCallNotFound", err)
	}
	if err := SaveVerdict(ctx, pool, "conv_1", contract.Verdict("maybe"), "x"); err == nil {
		t.Fatal("invalid verdict: want the CHECK constraint to reject it")
	}
	if verdict, _, _ := read(); verdict.String != "duress" {
		t.Fatalf("verdict after a rejected write = %v, want duress", verdict)
	}
}

func TestSaveVerdictWithContext(t *testing.T) {
	pool := freshDB(t)
	ctx := context.Background()
	if _, err := Migrate(ctx, pool, migrations.FS); err != nil {
		t.Fatal(err)
	}
	const incident = "9b2e4f1d-3c5a-4e7b-8d6f-1a2b3c4d5e6f"
	for _, q := range []string{
		`INSERT INTO devices (id, phone_number, duress_phrase) VALUES ('dev-1', '+584141234567', 'dile a la tía Carmen que llego tarde')`,
		`INSERT INTO incidents (id, device_id, trigger, branch, triggered_at) VALUES ('` + incident + `', 'dev-1', 'widget', 'call', now())`,
		`INSERT INTO calls (incident_id, conversation_id) VALUES ('` + incident + `', 'conv_1')`,
	} {
		if _, err := pool.ExecContext(ctx, q); err != nil {
			t.Fatalf("seed: %v", err)
		}
	}
	ficha := func() (lugar sql.NullString) {
		t.Helper()
		if err := pool.QueryRowContext(ctx, `SELECT ficha ->> 'lugar' FROM calls WHERE conversation_id = 'conv_1'`).Scan(&lugar); err != nil {
			t.Fatal(err)
		}
		return
	}

	c := &contract.CallContext{Lugar: "la autopista", Personas: "dos panas", Vehiculo: "un carro",
		Rumbo: contract.CallContextUnknown, Situacion: "va en un carro", Confianza: contract.ConfianzaMedia}
	if err := SaveVerdictWithContext(ctx, pool, "conv_1", contract.VerdictDuress, "said it", c); err != nil {
		t.Fatal(err)
	}
	if got := ficha(); got.String != "la autopista" {
		t.Fatalf("ficha lugar = %v", got)
	}
	// A repeated webhook without a context clears it.
	if err := SaveVerdict(ctx, pool, "conv_1", contract.VerdictNoDuress, "safe phrase"); err != nil {
		t.Fatal(err)
	}
	if got := ficha(); got.Valid {
		t.Fatalf("ficha after SaveVerdict = %v, want NULL", got)
	}
}
