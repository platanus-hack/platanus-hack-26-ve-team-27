package report

import (
	"bytes"
	"encoding/json"
	"html"
	"strings"
	"testing"

	"github.com/platanus-hack/platanus-hack-26-ve-team-27/server/internal/contract"
)

// withPhone adds what #125 stores: the alert's permission snapshot (battery
// as given, microphone off, every other one granted, plus a name the page
// does not know) and the device's permission events.
func withPhone(rec Record, battery bool) Record {
	rec.DeviceState = &contract.DeviceState{
		CapturedAt: at("2026-10-24T13:10:00.120Z"),
		Permissions: map[string]bool{
			contract.PermissionNotifications: true, contract.PermissionLocationFine: true, contract.PermissionLocationBackground: true,
			contract.PermissionMicrophone: false, contract.PermissionContacts: true, contract.PermissionSMS: true,
			contract.PermissionBatteryUnrestricted: battery, contract.PermissionHibernationExempt: true,
			contract.PermissionAccessibilityService: true, "exact_alarms": true,
		},
	}
	rec.PermissionEvents = []PermissionEvent{
		{Permission: contract.PermissionLocationFine, Granted: true, At: at("2026-10-24T12:02:00Z")},
		{Permission: contract.PermissionNotifications, Granted: true, At: at("2026-10-24T12:02:00Z")},
		{Permission: contract.PermissionLocationBackground, Granted: true, At: at("2026-10-24T12:02:30Z")},
		{Permission: contract.PermissionAccessibilityService, Granted: true, At: at("2026-10-24T12:05:00Z")},
		{Permission: "exact_alarms", Granted: true, At: at("2026-10-24T12:06:00Z")},
		{Permission: contract.PermissionLocationBackground, Granted: false, At: at("2026-10-24T12:40:00Z")},
		{Permission: contract.PermissionLocationBackground, Granted: true, At: at("2026-10-24T12:50:00Z")},
		{Permission: contract.PermissionSMS, Granted: true, At: at("2026-10-24T13:30:00Z")}, // after the case
	}
	return rec
}

func TestPhoneReady(t *testing.T) {
	if phoneReady(openRecord()) != nil {
		t.Fatal("a record without device_state has a ready node")
	}

	r := phoneReady(withPhone(openRecord(), true))
	if r == nil || r.Summary != "8 de 9 ✓" || r.Warn || r.Missing != "" || r.Time != "09:10:00" {
		t.Fatalf("battery on = %+v; want 8 de 9 (microphone off is not important), no warning, 09:10:00", r)
	}
	var labels []string
	for _, c := range r.Chips {
		labels = append(labels, c.Label)
	}
	want := "Notificaciones|Ubicación precisa|Ubicación siempre|Sin micrófono|Contactos|Mensajes SMS|Batería sin restricciones|Activa aunque no se use|Gesto de volumen"
	if got := strings.Join(labels, "|"); got != want {
		t.Errorf("chips = %s\nwant    %s (setup order, the unknown name left out)", got, want)
	}

	r = phoneReady(withPhone(openRecord(), false))
	if r == nil || r.Summary != "7 de 9 ✓" || !r.Warn || r.Missing != "Batería con restricciones" {
		t.Fatalf("battery off = %+v; want a warning naming it", r)
	}

	rec := withPhone(openRecord(), true)
	rec.DeviceState.CapturedAt = at("0001-01-01T00:00:00Z") // the phone sent a malformed one
	if r := phoneReady(rec); r.Time != clock(rec.Incident.TriggeredAt) {
		t.Errorf("no captured_at: time %s, want the gesture's", r.Time)
	}

	rec.DeviceState.Permissions = map[string]bool{"exact_alarms": true}
	if phoneReady(rec) != nil {
		t.Error("a snapshot with only unknown names has a ready node")
	}
}

func TestStagesStartWithTheReadyNode(t *testing.T) {
	plain := Stages(openRecord(), "WhatsApp", true)
	if plain[0].Key != "stage-trigger" {
		t.Fatalf("without device_state the track starts at %s", plain[0].Key)
	}
	st := Stages(withPhone(openRecord(), false), "WhatsApp", true)
	first := st[0]
	if len(st) != len(plain)+1 || first.Key != "stage-ready" || first.Label != "Teléfono listo" || first.Sub != "7 de 9 ✓" ||
		first.State != StageDone || first.Time != "09:10:00" || !first.Warn || first.Ready == nil {
		t.Fatalf("first node = %+v", first)
	}
	if st[1].Key != "stage-trigger" || st[1].Warn || st[1].Ready != nil {
		t.Errorf("second node = %+v", st[1])
	}
}

func TestPrepSteps(t *testing.T) {
	if prepSteps(openRecord()) != nil {
		t.Fatal("a device with no events has a strip")
	}
	var got []string
	for _, s := range prepSteps(withPhone(openRecord(), true)) {
		g := s.Label + " " + s.Time
		if s.Later {
			g += " later"
		}
		if s.Lost {
			g += " lost"
		}
		got = append(got, g)
	}
	want := []string{
		"Notificaciones 08:02", "Ubicación precisa 08:02", // same instant: setup's order
		"Ubicación siempre 08:02", "Gesto de volumen 08:05",
		"Ubicación solo con la app abierta 08:40 later lost", "Ubicación siempre 08:50 later",
	}
	if strings.Join(got, " | ") != strings.Join(want, " | ") {
		t.Fatalf("strip =\n%s\nwant\n%s (the unknown name and the change after the case left out)", strings.Join(got, "\n"), strings.Join(want, "\n"))
	}
}

// The family page shows the node and its popover, never the strip.
func TestFamilyPageShowsTheNodeNotTheStrip(t *testing.T) {
	snap, err := RenderSections(withPhone(openRecord(), false), Options{Token: testToken}, FamilySections)
	if err != nil {
		t.Fatal(err)
	}
	track := html.UnescapeString(snap.Sections["track"])
	for _, want := range []string{`popovertarget="phone-ready"`, `id="phone-ready" popover`, "node-warn", "Teléfono listo", "7 de 9 ✓",
		"Falta: Batería con restricciones", `perm-missing"><span class="perm-mark" aria-hidden="true">✗</span>Batería con restricciones`,
		`perm-off"><span class="perm-mark" aria-hidden="true">✗</span>Sin micrófono`, "Al pedir ayuda · 09:10:00 (reloj del teléfono)"} {
		if !strings.Contains(track, want) {
			t.Errorf("track lacks %q", want)
		}
	}
	all, _ := json.Marshal(snap.Sections)
	if bytes.Contains(all, []byte("Preparación del teléfono")) || bytes.Contains(all, []byte("exact_alarms")) {
		t.Error("the family page shows the strip or a raw permission name")
	}

	plain, err := RenderSections(openRecord(), Options{Token: testToken}, []string{"track"})
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(plain.Sections["track"], "phone-ready") || strings.Contains(plain.Sections["track"], "node-warn") {
		t.Error("a record without device_state renders the ready node")
	}
}

func TestCentralDetailShowsTheStrip(t *testing.T) {
	store := &fakeCentral{}
	store.setCase(withPhone(sealedRecord(), true), CaseExtras{})
	srv := centralServer(t, store, testCentralKey)
	_, body := cget(t, srv.URL+"/central/r/"+testIncidentID, login(t, srv))
	text := html.UnescapeString(body)
	for _, want := range []string{`data-live="c-prep"`, "Preparación del teléfono", "Notificaciones</span> <span class=\"c-prep-time\">08:02",
		`c-prep-step later lost`, "✗ Ubicación solo con la app abierta", "✓ Ubicación siempre</span> <span class=\"c-prep-time\">08:50",
		`popovertarget="phone-ready"`, "8 de 9 ✓"} {
		if !strings.Contains(text, want) {
			t.Errorf("detail lacks %q", want)
		}
	}

	store.setCase(sealedRecord(), CaseExtras{})
	_, body = cget(t, srv.URL+"/central/r/"+testIncidentID, login(t, srv))
	if strings.Contains(body, "Preparación del teléfono") || strings.Contains(body, "phone-ready") {
		t.Error("a case without #125 data shows the strip or the node")
	}
}

func TestPDFShowsThePhoneOutsideTheSeal(t *testing.T) {
	rec := withPhone(sealedRecord(), false)
	rep, err := buildPDF(rec, pdfOpts())
	if err != nil {
		t.Fatal(err)
	}
	if rep.Phone == nil || rep.Phone.Ready == nil || !rep.Phone.Ready.Warn || len(rep.Phone.Prep) != 6 {
		t.Fatalf("phone = %+v", rep.Phone)
	}
	b, err := renderPDF(rep)
	if err != nil || !bytes.HasPrefix(b, []byte("%PDF-1.")) {
		t.Fatalf("render: %v", err)
	}
	if rep, _ := buildPDF(sealedRecord(), pdfOpts()); rep.Phone != nil {
		t.Error("a record without #125 data has the section")
	}
}

// The seal signs ficha.json v1, which carries neither the snapshot nor the
// events: #125 does not ask to seal them, so the same case seals the same.
func TestSealIgnoresThePhone(t *testing.T) {
	rec := sealedRecord()
	pub := rec.Seal.PublicKey
	plain, err := Document(rec, rec.Seal.SealedAt, pub)
	if err != nil {
		t.Fatal(err)
	}
	with, err := Document(withPhone(rec, false), rec.Seal.SealedAt, pub)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(plain, with) {
		t.Fatal("the phone's permissions changed ficha.json")
	}
}
