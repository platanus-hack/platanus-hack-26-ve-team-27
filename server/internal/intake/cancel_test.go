package intake

import (
	"bytes"
	"encoding/json"
	"net/http"
	"testing"
	"time"

	"github.com/platanus-hack/platanus-hack-26-ve-team-27/server/internal/contract"
)

func cancelBody(t *testing.T, incident, device string) []byte {
	t.Helper()
	raw, err := json.Marshal(contract.IncidentCancel{IncidentID: incident, DeviceID: device})
	if err != nil {
		t.Fatal(err)
	}
	return raw
}

func (e *env) cancelledAt() *time.Time {
	e.t.Helper()
	var at *time.Time
	if err := e.pool.QueryRow(`SELECT cancelled_at FROM incidents WHERE id = $1`, incidentID).Scan(&at); err != nil {
		e.t.Fatal(err)
	}
	return at
}

func TestCancelMarksTheIncidentAndStopsItsCall(t *testing.T) {
	e := newEnv(t)
	if rec := e.post("/incidents", appAlert(incidentID, 1)); rec.Code != http.StatusCreated {
		t.Fatalf("POST /incidents = %d %s", rec.Code, rec.Body)
	}
	if e.cancelledAt() != nil {
		t.Fatal("a new incident is already cancelled")
	}

	rec := e.post("/incidents/"+incidentID+"/cancel", cancelBody(t, incidentID, deviceID))
	if rec.Code != http.StatusNoContent {
		t.Fatalf("cancel = %d %s, want 204", rec.Code, rec.Body)
	}
	first := e.cancelledAt()
	if first == nil {
		t.Fatal("cancelled_at not set")
	}
	if len(e.cancelled.ids) != 1 || e.cancelled.ids[0] != incidentID {
		t.Fatalf("call canceller got %v, want the incident", e.cancelled.ids)
	}

	// Cancelling again succeeds, keeps the first time and tries the hang-up
	// again (the first may have failed).
	rec = e.post("/incidents/"+incidentID+"/cancel", cancelBody(t, incidentID, deviceID))
	if rec.Code != http.StatusNoContent {
		t.Fatalf("second cancel = %d %s, want 204", rec.Code, rec.Body)
	}
	if again := e.cancelledAt(); again == nil || !again.Equal(*first) {
		t.Errorf("cancelled_at = %v, want the first time %v", again, first)
	}
	if len(e.cancelled.ids) != 2 {
		t.Errorf("call canceller ran %d times, want 2", len(e.cancelled.ids))
	}
}

func TestCancelRefusals(t *testing.T) {
	e := newEnv(t)
	if rec := e.post("/incidents", appAlert(incidentID, 1)); rec.Code != http.StatusCreated {
		t.Fatalf("POST /incidents = %d %s", rec.Code, rec.Body)
	}
	const unknown = "1a2b3c4d-5e6f-4a7b-8c9d-0e1f2a3b4c5d"
	cases := []struct {
		name string
		path string
		body []byte
		want int
	}{
		{"another device", "/incidents/" + incidentID + "/cancel", cancelBody(t, incidentID, "other-device"), http.StatusForbidden},
		{"unknown incident", "/incidents/" + unknown + "/cancel", cancelBody(t, unknown, deviceID), http.StatusNotFound},
		{"path and body differ", "/incidents/" + unknown + "/cancel", cancelBody(t, incidentID, deviceID), http.StatusBadRequest},
		{"incident_id not a UUID", "/incidents/nope/cancel", cancelBody(t, "nope", deviceID), http.StatusBadRequest},
		{"no device_id", "/incidents/" + incidentID + "/cancel", cancelBody(t, incidentID, ""), http.StatusBadRequest},
		{"bad JSON", "/incidents/" + incidentID + "/cancel", []byte(`{`), http.StatusBadRequest},
		{"unknown field", "/incidents/" + incidentID + "/cancel", []byte(`{"incident_id":"` + incidentID + `","device_id":"` + deviceID + `","x":1}`), http.StatusBadRequest},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if rec := e.post(c.path, c.body); rec.Code != c.want {
				t.Errorf("cancel = %d %s, want %d", rec.Code, rec.Body, c.want)
			}
		})
	}
	if e.cancelledAt() != nil {
		t.Error("a refused cancel marked the incident")
	}
	if len(e.cancelled.ids) != 0 {
		t.Errorf("a refused cancel reached the call canceller: %v", e.cancelled.ids)
	}
}

// The sample file is the frozen shape of the request.
func TestCancelAcceptsTheContractSample(t *testing.T) {
	e := newEnv(t)
	if rec := e.post("/incidents", appAlert(incidentID, 1)); rec.Code != http.StatusCreated {
		t.Fatalf("POST /incidents = %d %s", rec.Code, rec.Body)
	}
	if rec := e.post("/incidents/"+incidentID+"/cancel", bytes.TrimSpace(sample(t, "incident_cancel.json"))); rec.Code != http.StatusNoContent {
		t.Fatalf("cancel with the sample = %d %s", rec.Code, rec.Body)
	}
}
