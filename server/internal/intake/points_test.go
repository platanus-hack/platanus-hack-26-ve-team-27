package intake

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"testing"
)

// appPoint is one fix as the app's ContractJson writes it: compact, with a
// Location's full-precision doubles and the Float accuracy and speed widened
// to doubles, the longest a real fix gets.
func appPoint(sec int) string {
	return fmt.Sprintf(`{"lat":10.496123399999999,"lng":-66.85312340000001,"accuracy_m":3.7899999618530273,"speed_mps":0.41999998688697815,"recorded_at":"2026-10-24T09:%02d:%02d.253Z"}`, 10+sec/60, sec%60)
}

func appPoints(n int) string {
	points := make([]string, n)
	for i := range points {
		points[i] = appPoint(i)
	}
	return "[" + strings.Join(points, ",") + "]"
}

// appDeviceState is the permission snapshot the app adds to every alert
// (#125): all ten permissions, the most it sends.
const appDeviceState = `{"captured_at":"2026-10-24T09:10:00.251Z","permissions":{"location_fine":true,"location_background":true,` +
	`"microphone":true,"contacts":true,"sms":true,"notifications":true,"accessibility_service":true,` +
	`"battery_unrestricted":false,"full_screen_intent":true,"hibernation_exempt":true}}`

// appAlert is the AlertEvent the app sends, with n fixes (it sends 1).
func appAlert(id string, n int) []byte {
	return []byte(`{"incident_id":"` + id + `","device_id":"` + deviceID + `","trigger":"volume_pattern","triggered_at":"2026-10-24T09:10:00.253Z","points":` + appPoints(n) + `,"has_audio":false,"device_state":` + appDeviceState + `}`)
}

// appUpdate is a LocationUpdate with n fixes (the app sends 1), recorded
// from second from on.
func appUpdate(id string, from, n int) []byte {
	points := make([]string, n)
	for i := range points {
		points[i] = appPoint(from + i)
	}
	return []byte(`{"incident_id":"` + id + `","device_id":"` + deviceID + `","points":[` + strings.Join(points, ",") + `]}`)
}

// What the app sends passes, and so does the cap itself, even indented.
func TestRequestsTheAppSendsFitTheCaps(t *testing.T) {
	e := newEnv(t)
	alert := appAlert(incidentID, 1)
	if len(alert) >= 1<<10 {
		t.Errorf("the app's alert is %d bytes; the maxBodyBytes comment says under 1 KiB", len(alert))
	}
	if rec := e.post("/incidents", alert); rec.Code != http.StatusCreated {
		t.Fatalf("app alert = %d %s", rec.Code, rec.Body)
	}
	if rec := e.post("/incidents/"+incidentID+"/points", appUpdate(incidentID, 100, 1)); rec.Code != http.StatusNoContent {
		t.Fatalf("app update = %d %s", rec.Code, rec.Body)
	}

	var indented bytes.Buffer
	if err := json.Indent(&indented, appUpdate(incidentID, 200, maxPoints), "", "    "); err != nil {
		t.Fatal(err)
	}
	if indented.Len() > maxBodyBytes {
		t.Fatalf("%d fixes indented are %d bytes, over maxBodyBytes %d", maxPoints, indented.Len(), maxBodyBytes)
	}
	if rec := e.post("/incidents/"+incidentID+"/points", indented.Bytes()); rec.Code != http.StatusNoContent {
		t.Fatalf("update with %d fixes = %d %s", maxPoints, rec.Code, rec.Body)
	}
	const other = "1a2b3c4d-5e6f-4a7b-8c9d-0e1f2a3b4c5d"
	if rec := e.post("/incidents", appAlert(other, maxPoints)); rec.Code != http.StatusCreated {
		t.Fatalf("alert with %d fixes = %d %s", maxPoints, rec.Code, rec.Body)
	}
	if n := e.count(`SELECT count(*) FROM gps_points`); n != 2+2*maxPoints {
		t.Errorf("gps_points = %d, want %d (every fix sent)", n, 2+2*maxPoints)
	}
}

func TestTooManyPointsAre400AndStoreNothing(t *testing.T) {
	e := newEnv(t)
	if rec := e.post("/incidents", appAlert(incidentID, 1)); rec.Code != http.StatusCreated {
		t.Fatalf("POST /incidents = %d %s", rec.Code, rec.Body)
	}
	const other = "1a2b3c4d-5e6f-4a7b-8c9d-0e1f2a3b4c5d"
	for _, tc := range []struct {
		name, path string
		body       []byte
	}{
		{"alert", "/incidents", appAlert(other, maxPoints+1)},
		{"update", "/incidents/" + incidentID + "/points", appUpdate(incidentID, 100, maxPoints+1)},
	} {
		rec := e.post(tc.path, tc.body)
		want := fmt.Sprintf("points has %d fixes; send at most %d per request", maxPoints+1, maxPoints)
		if rec.Code != http.StatusBadRequest || !strings.Contains(rec.Body.String(), want) {
			t.Errorf("%s with %d fixes = %d %s, want 400 %q", tc.name, maxPoints+1, rec.Code, rec.Body, want)
		}
	}
	if n := e.count(`SELECT count(*) FROM gps_points`); n != 1 {
		t.Errorf("gps_points = %d, want only the first alert's 1", n)
	}
	if n := e.count(`SELECT count(*) FROM incidents`); n != 1 {
		t.Errorf("incidents = %d, want only the first", n)
	}
	if len(e.fake.notifies)+len(e.fake.calls) != 1 {
		t.Errorf("handoffs = %d calls, %d notifies; want only the first alert's", len(e.fake.calls), len(e.fake.notifies))
	}
}

// A 32 KiB body is cut off before it is decoded: 1 MiB used to be read whole.
func TestLargeBodyIs400(t *testing.T) {
	e := newEnv(t)
	body := appAlert(incidentID, 1)
	body = append(body[:len(body)-1], []byte(`,"pad":"`+strings.Repeat("x", 32<<10)+`"}`)...)
	rec := e.post("/incidents", body)
	if rec.Code != http.StatusBadRequest || !strings.Contains(rec.Body.String(), "request body too large") {
		t.Errorf("POST /incidents of %d bytes = %d %s, want 400 request body too large", len(body), rec.Code, rec.Body)
	}
}
