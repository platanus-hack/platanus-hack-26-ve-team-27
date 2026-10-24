package main

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"crypto/sha256"
	"encoding/json"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/platanus-hack/platanus-hack-26-ve-team-27/server/internal/call"
	"github.com/platanus-hack/platanus-hack-26-ve-team-27/server/internal/config"
	"github.com/platanus-hack/platanus-hack-26-ve-team-27/server/internal/contract"
	"github.com/platanus-hack/platanus-hack-26-ve-team-27/server/internal/db"
	"github.com/platanus-hack/platanus-hack-26-ve-team-27/server/internal/dbtest"
	"github.com/platanus-hack/platanus-hack-26-ve-team-27/server/internal/emergency/ingest"
	"github.com/platanus-hack/platanus-hack-26-ve-team-27/server/internal/emergency/payload"
	"github.com/platanus-hack/platanus-hack-26-ve-team-27/server/internal/intake"
	"github.com/platanus-hack/platanus-hack-26-ve-team-27/server/migrations"
)

// notified hands each family notification to the test.
type notified chan intake.Incident

func (n notified) Notify(_ context.Context, inc intake.Incident) { n <- inc }

// TestMeshRelayUploadOpensAnIncident walks the whole server path of an
// offline emergency relayed over the BLE mesh: a registered device's sealed
// alert uploaded by a relay (#60) is stored, acked, opened as an incident with
// its fix and handed to the family notifier once.
func TestMeshRelayUploadOpensAnIncident(t *testing.T) {
	pool := dbtest.Fresh(t)
	if _, err := db.Migrate(context.Background(), pool, migrations.FS); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	offline := testOffline(t)
	notes := make(notified, 4)
	mux := newMux(pool, notes, call.NewRoutes(config.Config{}, nil), offline, nil)

	seed := sha256.Sum256([]byte("offline-incident-test-device"))
	dev := ed25519.NewKeyFromSeed(seed[:])
	const deviceID = "3f1c9a52-7b1e-4c8a-9a0e-2d6b8f4e1a77"
	reg, _ := json.Marshal(contract.DeviceRegistration{
		DeviceID:         deviceID,
		PhoneNumber:      "+584141234567",
		FamilyNumbers:    []string{"+584241112233"},
		DuressPhrase:     "frase",
		Ed25519PublicKey: dev.Public().(ed25519.PublicKey),
		Ed25519Signature: ed25519.Sign(dev, contract.RegistrationSignedMessage(deviceID)),
	})
	if rec := serve(mux, "/devices", reg); rec.Code != http.StatusNoContent {
		t.Fatalf("POST /devices = %d %s", rec.Code, rec.Body)
	}

	signed, err := payload.Sign(payload.Payload{
		Kind:        payload.KindAlert,
		DeviceID:    payload.DeviceID(dev.Public().(ed25519.PublicKey)),
		EmergencyID: 0x00c0ffee12345678,
		LatE5:       1048801,
		LonE5:       -6687919,
		LocAgeS:     5,
		Timestamp:   uint32(time.Now().Unix()),
	}, dev)
	if err != nil {
		t.Fatal(err)
	}
	box, err := payload.Seal(rand.Reader, offline.keys.Sealing.PublicKey(), signed)
	if err != nil {
		t.Fatal(err)
	}
	upload, _ := json.Marshal(map[string]any{"text": payload.MeshSealedText(box), "hops": 1})
	for i := 0; i < 2; i++ { // the second relay's copy is the same emergency
		rec := serve(mux, "/emergencies/relay", upload)
		if rec.Code != http.StatusOK || !strings.HasPrefix(rec.Body.String(), "MAMA1:ACK:") {
			t.Fatalf("POST /emergencies/relay = %d %s", rec.Code, rec.Body)
		}
	}

	var inc intake.Incident
	select {
	case inc = <-notes:
	case <-time.After(5 * time.Second):
		t.Fatal("the family was never notified")
	}
	wantID := intake.OfflineIncidentID(ingest.FormatID(0x00c0ffee12345678))
	if inc.ID != wantID || inc.DeviceID != deviceID || inc.Branch != contract.BranchImmediateAlert ||
		inc.Location.Lat != 10.48801 || inc.Location.Lng != -66.87919 {
		t.Fatalf("notified %+v", inc)
	}
	var trigger string
	var points int
	if err := pool.QueryRow(`SELECT i.trigger, count(p.*) FROM incidents i LEFT JOIN gps_points p ON p.incident_id = i.id
		WHERE i.id = $1 GROUP BY i.trigger`, wantID).Scan(&trigger, &points); err != nil {
		t.Fatal(err)
	}
	if trigger != "offline" || points != 1 {
		t.Fatalf("trigger=%q points=%d", trigger, points)
	}
	select {
	case again := <-notes:
		t.Fatalf("notified twice: %+v", again)
	case <-time.After(300 * time.Millisecond):
	}
}
