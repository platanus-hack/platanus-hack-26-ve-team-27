package report

import (
	"bufio"
	"context"
	"encoding/json"
	"html"
	"io"
	"math"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/platanus-hack/platanus-hack-26-ve-team-27/server/internal/contract"
)

const testCentralKey = "central-test-key"

// fakeCentral is a CentralStore the test changes while a page streams.
type fakeCentral struct {
	mu     sync.Mutex
	items  []CaseItem
	recs   map[string]Record
	extras map[string]CaseExtras
}

func (f *fakeCentral) Cases(_ context.Context, limit int) ([]CaseItem, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	out := append([]CaseItem(nil), f.items...)
	if len(out) > limit {
		out = out[:limit]
	}
	return out, nil
}

func (f *fakeCentral) Load(_ context.Context, id string) (Record, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	rec, ok := f.recs[id]
	if !ok {
		return Record{}, ErrUnknownIncident
	}
	return rec, nil
}

func (f *fakeCentral) Extras(_ context.Context, id string) (CaseExtras, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if _, ok := f.recs[id]; !ok {
		return CaseExtras{}, ErrUnknownIncident
	}
	return f.extras[id], nil
}

func (f *fakeCentral) Ensure(_ context.Context, id string, _ int) (Link, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if _, ok := f.recs[id]; !ok {
		return Link{}, ErrUnknownIncident
	}
	return Link{Token: testToken}, nil
}

func (f *fakeCentral) setCases(items ...CaseItem) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.items = items
}

func (f *fakeCentral) setCase(rec Record, x CaseExtras) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.recs == nil {
		f.recs, f.extras = map[string]Record{}, map[string]CaseExtras{}
	}
	f.recs[rec.Incident.ID], f.extras[rec.Incident.ID] = rec, x
}

// centralNow is the tests' clock: the fixture's case is live at it.
var centralNow = at("2026-10-24T13:11:40Z")

func centralServer(t *testing.T, store CentralStore, key string) *httptest.Server {
	t.Helper()
	h := NewHandler(fakeSource{}, Options{CanSeal: true, Channel: "WhatsApp"}).
		WithCentral(&Central{Key: key, Store: store})
	h.LivePoll, h.KeepAlive = 10*time.Millisecond, time.Hour
	h.Now = func() time.Time { return centralNow }
	mux := http.NewServeMux()
	h.Register(mux)
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	return srv
}

// noRedirect is a client that shows redirects instead of following them.
var noRedirect = &http.Client{CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}

func cget(t *testing.T, u string, cookie *http.Cookie) (*http.Response, string) {
	t.Helper()
	req, _ := http.NewRequest(http.MethodGet, u, nil)
	if cookie != nil {
		req.AddCookie(cookie)
	}
	resp, err := noRedirect.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	b, _ := io.ReadAll(resp.Body)
	return resp, string(b)
}

// login opens /central/?key= and returns the cookie it sets.
func login(t *testing.T, srv *httptest.Server) *http.Cookie {
	t.Helper()
	resp, _ := cget(t, srv.URL+"/central/?key="+testCentralKey, nil)
	for _, c := range resp.Cookies() {
		if c.Name == centralCookie {
			return c
		}
	}
	t.Fatalf("no %s cookie: %d %v", centralCookie, resp.StatusCode, resp.Header)
	return nil
}

func checkPrivacy(t *testing.T, name string, h http.Header) {
	t.Helper()
	if h.Get("Referrer-Policy") != "no-referrer" || !strings.Contains(h.Get("X-Robots-Tag"), "noindex") ||
		h.Get("Cache-Control") != "no-store" || !strings.Contains(h.Get("Content-Security-Policy"), "default-src 'none'") {
		t.Errorf("%s: missing privacy headers: %v", name, h)
	}
}

// Without the key the dashboard does not exist: 404 for every route, with
// no key, a wrong key or a forged cookie. The right key sets an HttpOnly,
// Secure, SameSite=Strict cookie and redirects to the same URL without it;
// the cookie then opens every route.
func TestCentralNeedsTheKey(t *testing.T) {
	store := &fakeCentral{}
	store.setCase(openRecord(), CaseExtras{})
	srv := centralServer(t, store, testCentralKey)
	routes := []string{"/central", "/central/", "/central/live", "/central/estado.json",
		"/central/r/" + testIncidentID, "/central/r/" + testIncidentID + "/live", "/central/r/" + testIncidentID + "/estado.json"}

	for _, r := range routes {
		if resp, _ := cget(t, srv.URL+r, nil); resp.StatusCode != http.StatusNotFound {
			t.Errorf("no key: GET %s = %d, want 404", r, resp.StatusCode)
		} else {
			checkPrivacy(t, "no key "+r, resp.Header)
		}
		if resp, _ := cget(t, srv.URL+r+"?key=wrong", nil); resp.StatusCode != http.StatusNotFound || len(resp.Cookies()) > 0 {
			t.Errorf("wrong key: GET %s = %d %v, want 404 and no cookie", r, resp.StatusCode, resp.Cookies())
		}
		forged := &http.Cookie{Name: centralCookie, Value: testCentralKey}
		if resp, _ := cget(t, srv.URL+r, forged); resp.StatusCode != http.StatusNotFound {
			t.Errorf("forged cookie: GET %s = %d, want 404", r, resp.StatusCode)
		}
	}

	resp, _ := cget(t, srv.URL+"/central/?key="+url.QueryEscape(testCentralKey)+"&tema=claro", nil)
	if resp.StatusCode != http.StatusSeeOther || resp.Header.Get("Location") != "/central/?tema=claro" {
		t.Fatalf("right key = %d to %q, want 303 to the URL without the key", resp.StatusCode, resp.Header.Get("Location"))
	}
	checkPrivacy(t, "right key", resp.Header)
	var ck *http.Cookie
	for _, c := range resp.Cookies() {
		if c.Name == centralCookie {
			ck = c
		}
	}
	if ck == nil || !ck.HttpOnly || !ck.Secure || ck.SameSite != http.SameSiteStrictMode || ck.Path != "/central" || ck.MaxAge <= 0 {
		t.Fatalf("cookie = %+v, want HttpOnly, Secure, SameSite=Strict on /central", ck)
	}
	if strings.Contains(ck.Value, testCentralKey) {
		t.Error("the cookie carries the key itself")
	}
	for _, r := range routes[1:] {
		if strings.HasSuffix(r, "/live") {
			continue // streams; TestCentralListStreamsANewCase opens one
		}
		if resp, _ := cget(t, srv.URL+r, ck); resp.StatusCode != http.StatusOK {
			t.Errorf("cookie: GET %s = %d, want 200", r, resp.StatusCode)
		} else {
			checkPrivacy(t, "cookie "+r, resp.Header)
		}
	}
	if resp, _ := cget(t, srv.URL+"/central/r/00000000-0000-4000-8000-000000000000", ck); resp.StatusCode != http.StatusNotFound {
		t.Errorf("unknown case = %d, want 404", resp.StatusCode)
	}
	if resp, _ := cget(t, srv.URL+"/central/r/not-a-uuid", ck); resp.StatusCode != http.StatusNotFound {
		t.Errorf("malformed id = %d, want 404", resp.StatusCode)
	}

	// Another key's cookie no longer works: changing CENTRAL_KEY logs out.
	other := centralServer(t, store, "another-key")
	if resp, _ := cget(t, other.URL+"/central/", ck); resp.StatusCode != http.StatusNotFound {
		t.Errorf("cookie of another key = %d, want 404", resp.StatusCode)
	}
}

// With no key the dashboard is not mounted at all.
func TestCentralOffWithoutKey(t *testing.T) {
	store := &fakeCentral{}
	for _, srv := range []*httptest.Server{centralServer(t, store, ""), func() *httptest.Server {
		mux := http.NewServeMux()
		NewHandler(fakeSource{}, Options{}).Register(mux) // WithCentral never called
		s := httptest.NewServer(mux)
		t.Cleanup(s.Close)
		return s
	}()} {
		if resp, _ := cget(t, srv.URL+"/central/?key=", nil); resp.StatusCode != http.StatusNotFound {
			t.Errorf("GET /central/?key= = %d, want 404", resp.StatusCode)
		}
	}
}

// caseAt is a list item for state tests: a call-branch case that reached
// the server at created.
func caseAt(id string, created time.Time) CaseItem {
	return CaseItem{ID: id, Trigger: contract.TriggerVolumePattern, Branch: contract.BranchCall,
		TriggeredAt: created.Add(-400 * time.Millisecond), CreatedAt: created, LastAt: created, Phone: "+584141234567"}
}

func TestCaseStates(t *testing.T) {
	now := centralNow
	recent := now.Add(-2 * time.Minute)
	notified := ptr(recent.Add(time.Minute))
	call := func(status string, requested time.Time) *Call { return &Call{Status: status, RequestedAt: requested} }
	cases := []struct {
		name   string
		edit   func(*CaseItem)
		want   string
		line   string
		family string
	}{
		{"just arrived, no call yet", func(*CaseItem) {}, CaseActive, "Mamá va a llamar…", ""},
		{"Mamá is dialing", func(c *CaseItem) { c.Call = call("requested", recent) }, CaseCalling, "Mamá está llamando", ""},
		{"talking", func(c *CaseItem) { c.Call = call("in_progress", recent); c.Call.PickedUpAt = ptr(recent) }, CaseCalling, "Hablando con Mamá", ""},
		{"a call left in progress for too long", func(c *CaseItem) { c.Call = call("in_progress", now.Add(-20*time.Minute)); c.LastAt = recent }, CaseActive, "Esperando veredicto…", ""},
		{"call ended, waiting for the verdict", func(c *CaseItem) { c.Call = call("done", recent) }, CaseActive, "Esperando veredicto…", ""},
		{"duress", func(c *CaseItem) {
			c.Call, c.Verdict, c.VerdictReason, c.NotifiedAt = call("done", recent), contract.VerdictDuress, "the user said the duress phrase", notified
			c.Deliveries, c.Delivered = 2, 2
		}, CaseAlert, "Coacción: dijo la frase acordada", "✓✓ 2 de 2"},
		{"duress stays an alert once sealed", func(c *CaseItem) {
			c.Call, c.Verdict, c.SealedAt, c.NotifiedAt = call("done", recent), contract.VerdictDuress, ptr(now), notified
		}, CaseAlert, "Coacción", "Avisada"},
		{"said it is fine: nobody notified", func(c *CaseItem) { c.Call, c.Verdict = call("done", recent), contract.VerdictNoDuress }, CaseCancelled, "Dijo que está bien · no se avisó a nadie", ""},
		{"cancelled on the incident row", func(c *CaseItem) { c.Cancelled = true }, CaseCancelled, "Canceló la alerta · no se avisó a nadie", ""},
		{"no_duress that was notified (before #106) is not a cancellation", func(c *CaseItem) {
			c.Call, c.Verdict, c.NotifiedAt, c.SealedAt = call("done", recent), contract.VerdictNoDuress, notified, ptr(now)
		}, CaseClosed, "Sin coacción", "Avisada"},
		{"unclear, sealed", func(c *CaseItem) {
			c.Call, c.Verdict, c.NotifiedAt, c.SealedAt = call("done", recent), contract.VerdictUnclear, notified, ptr(now)
			c.Deliveries = 2
		}, CaseClosed, "No está claro", "✓ 2 enviados"},
		{"quiet for 30 minutes", func(c *CaseItem) { c.CreatedAt, c.LastAt = now.Add(-31*time.Minute), now.Add(-31*time.Minute) }, CaseClosed, "No se registró la llamada", ""},
		{"immediate alert", func(c *CaseItem) { c.Branch, c.NotifiedAt = contract.BranchImmediateAlert, notified }, CaseActive, "Quieta: aviso inmediato", "Avisada"},
		{"call failed", func(c *CaseItem) { c.Call = call("failed", recent) }, CaseActive, "La llamada no conectó", ""},
	}
	for _, tc := range cases {
		c := caseAt(testIncidentID, recent)
		tc.edit(&c)
		r := caseRow(c, now)
		if r.State != tc.want || !strings.HasPrefix(r.Line, tc.line) || r.Family != tc.family {
			t.Errorf("%s: state %q line %q family %q, want %q %q… %q", tc.name, r.State, r.Line, r.Family, tc.want, tc.line, tc.family)
		}
	}
}

func TestCaseRowShowsWhoAndWhere(t *testing.T) {
	c := caseAt(testIncidentID, centralNow.Add(-30*time.Second))
	c.UserName = " María  Rodríguez "
	c.Last = &Point{Lat: 10.4961, Lng: -66.8531, AccuracyM: ptr(12.5), SpeedMPS: ptr(1.2), RecordedAt: c.CreatedAt, ReceivedAt: c.CreatedAt}
	r := caseRow(c, centralNow)
	if r.Name != "María  Rodríguez" || r.Initials != "MR" || r.Number != "+58 414 ···67" || !r.New ||
		r.Place != "10.49610, -66.85310" || r.PlaceNote != "±12 m · caminando · 4 km/h" || r.URL != "/central/r/"+testIncidentID {
		t.Errorf("row = %+v", r)
	}
	if strings.Contains(r.Number, "1234567") {
		t.Error("the row shows the full number")
	}
	c.CreatedAt = centralNow.Add(-3 * time.Minute)
	if caseRow(c, centralNow).New {
		t.Error("a 3-minute-old case is still new")
	}
}

// The list page and its stream: newest first, each row's state, and a case
// that arrives while the page is open comes as one update with the new row
// first, without reloading.
func TestCentralListStreamsANewCase(t *testing.T) {
	store := &fakeCentral{}
	older := caseAt("11111111-1111-4111-8111-111111111111", centralNow.Add(-40*time.Minute))
	older.UserName, older.Verdict, older.NotifiedAt = "Sofía", contract.VerdictDuress, ptr(centralNow.Add(-39*time.Minute))
	store.setCases(older)
	srv := centralServer(t, store, testCentralKey)
	ck := login(t, srv)

	resp, body := cget(t, srv.URL+"/central/", ck)
	if resp.StatusCode != http.StatusOK || !strings.Contains(body, `data-live-url="/central/live"`) || !strings.Contains(body, "Sofía") ||
		!strings.Contains(body, `class="c-state c-state-alerta"`) || !strings.Contains(body, `data-theme="dark"`) {
		t.Fatalf("list page: %d\n%s", resp.StatusCode, body)
	}

	req, _ := http.NewRequest(http.MethodGet, srv.URL+"/central/live", nil)
	req.AddCookie(ck)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	stream, err := http.DefaultClient.Do(req.WithContext(ctx))
	if err != nil {
		t.Fatal(err)
	}
	defer stream.Body.Close()
	checkPrivacy(t, "list stream", stream.Header)
	msgs := make(chan sseMessage, 16)
	go readSSE(bufio.NewReader(stream.Body), msgs)
	first := decodeAny(t, next(t, msgs))
	if first.State != "list" || first.End || strings.Count(first.Sections["c-list"], `class="c-row `) != 1 {
		t.Fatalf("first update: %+v", first)
	}

	fresh := caseAt(testIncidentID, centralNow.Add(-5*time.Second))
	fresh.UserName = "María Rodríguez"
	fresh.Call = &Call{Status: "in_progress", RequestedAt: centralNow.Add(-3 * time.Second), RingingAt: ptr(centralNow.Add(-2 * time.Second))}
	store.setCases(fresh, older)
	second := decodeAny(t, next(t, msgs))
	list := second.Sections["c-list"]
	iNew, iOld := strings.Index(list, `data-key="case-`+testIncidentID+`"`), strings.Index(list, `data-key="case-`+older.ID+`"`)
	if iNew < 0 || iOld < 0 || iNew > iOld {
		t.Fatalf("the new case is not first:\n%s", list)
	}
	if !strings.Contains(list, "c-row-llamada c-row-new") || !strings.Contains(list, "data-new") || !strings.Contains(html.UnescapeString(list), "Sonando") {
		t.Errorf("the new row lacks its state or new tag:\n%s", list)
	}
	if !strings.Contains(second.Sections["c-counts"], `data-key="v-1" data-reveal class="c-count-n">1</span><span data-key="l-En llamada" class="c-count-label">En llamada`) {
		t.Errorf("counters: %s", second.Sections["c-counts"])
	}
}

func decodeAny(t *testing.T, m sseMessage) Snapshot {
	t.Helper()
	if m.event != "update" {
		t.Fatalf("event = %q, want update", m.event)
	}
	var s Snapshot
	if err := json.Unmarshal([]byte(m.data), &s); err != nil {
		t.Fatal(err)
	}
	return s
}

const testFicha = `{"lugar": "Autopista Caracas-La Guaira", "personas": "Dos hombres", "vehículo": "Carro gris",
	"rumbo": "no se sabe", "situacion": "Parece que se la llevan en un carro.", "confianza": "Alta"}`

// The detail renders the case's sections, with and without #106's ficha
// and #104's name: the ficha's one-line situation goes in the header, under
// the name, and nothing stands there without one. The context card, the
// vertical timeline and the points table are gone: the stage track is the
// only timeline, and the route carries the points' times.
func TestCentralCaseWithAndWithoutSituationAndName(t *testing.T) {
	store := &fakeCentral{}
	rec := sealedRecord()
	store.setCase(rec, CaseExtras{})
	srv := centralServer(t, store, testCentralKey)
	ck := login(t, srv)

	resp, body := cget(t, srv.URL+"/central/r/"+testIncidentID, ck)
	text := html.UnescapeString(body)
	for _, want := range []string{
		`data-live-url="/central/r/` + testIncidentID + `/live"`, `data-state="alerta"`,
		"+58 414 ···67", "Sin nombre registrado", "Coacción: dijo la frase acordada", // who and the verdict
		"/i/" + testToken + "/audio.mp3?llamada=1", "La frase acordada", // the recording and transcript
		"Ubicación en el tiempo", "Abrir en Maps", "668 m recorridos", // the route
		`class="rt newest"`, ">09:11:30</text>", // the newest point's time on it (VET)
		"/i/" + testToken + "/reporte.pdf",
	} {
		if !strings.Contains(text, want) {
			t.Errorf("detail lacks %q", want)
		}
	}
	for _, gone := range []string{"Contexto", "c-context", "Línea de tiempo", `data-live="timeline"`, `class="points"`, `data-live="points"`,
		"c-situation-line", "Confianza", "Recibido 0,4 s después"} {
		if strings.Contains(text, gone) {
			t.Errorf("detail still has %q", gone)
		}
	}
	if resp.StatusCode != http.StatusOK || strings.Contains(text, "+584141234567") || strings.Contains(text, rec.Incident.DeviceID) {
		t.Errorf("detail = %d, or it shows the full number or the device id", resp.StatusCode)
	}

	rec.Device.UserName = "María Rodríguez"
	store.setCase(rec, CaseExtras{Ficha: json.RawMessage(testFicha)})
	_, body = cget(t, srv.URL+"/central/r/"+testIncidentID, ck)
	text = html.UnescapeString(body)
	name, sit := strings.Index(text, `class="c-name-big">María Rodríguez</h1>`), strings.Index(text, `class="c-situation-line">Parece que se la llevan en un carro.</p>`)
	if name < 0 || sit < name || !strings.Contains(text, "<title>María Rodríguez · Central · MAMÁ</title>") {
		t.Errorf("the situation is not under the name (name at %d, situation at %d)", name, sit)
	}
	for _, gone := range []string{"Autopista Caracas-La Guaira", "Dos hombres", "Carro gris", "Confianza", "Sin nombre registrado"} {
		if strings.Contains(text, gone) {
			t.Errorf("detail with a ficha shows %q", gone)
		}
	}
}

// The situation arrives live: the header section changes when the ficha
// is stored, with the new line keyed so it enters by itself.
func TestCentralSituationArrivesLive(t *testing.T) {
	rec := openRecord()
	opts := Options{Token: testToken, Channel: "WhatsApp", CanSeal: true, Now: centralNow}
	before, err := centralCaseSnapshot(buildCentralCase(rec, CaseExtras{}, opts), centralNow)
	if err != nil {
		t.Fatal(err)
	}
	after, err := centralCaseSnapshot(buildCentralCase(rec, CaseExtras{Ficha: json.RawMessage(testFicha)}, opts), centralNow)
	if err != nil {
		t.Fatal(err)
	}
	if before.Version == after.Version || strings.Contains(before.Sections["c-head"], "c-situation-line") ||
		!strings.Contains(after.Sections["c-head"], `data-key="situation-Parece que se la llevan en un carro."`) {
		t.Errorf("header before:\n%s\nafter:\n%s", before.Sections["c-head"], after.Sections["c-head"])
	}
	for _, name := range CentralCaseSections {
		if name == "timeline" || name == "points" || name == "c-context" {
			t.Errorf("the detail still streams %s", name)
		}
	}
}

// While the call is on, the detail shows the turns stored so far (#97
// appends them live), the phrase marked as soon as it is said; the family
// page keeps them for the end of the call.
func TestCentralCaseShowsTurnsDuringTheCall(t *testing.T) {
	rec := openRecord()
	c := &rec.Calls[0]
	c.Status, c.EndedAt, c.DurationSecs, c.Verdict, c.VerdictAt, c.VerdictReason = "in_progress", nil, nil, "", nil, ""
	c.Transcript = c.Transcript[:4]
	rec.Incident.NotifiedAt = nil
	rec.Points = rec.Points[:2]
	opts := Options{Token: testToken, Channel: "WhatsApp", CanSeal: true, Now: at("2026-10-24T13:10:50Z")}
	d := buildCentralCase(rec, CaseExtras{}, opts)
	if d.Case.State != CaseCalling || d.Call == nil || len(d.Call.Turns) != 4 || !d.Call.Turns[3].Phrase || d.Situation != "" {
		t.Fatalf("state %s call %+v situation %q", d.Case.State, d.Call, d.Situation)
	}
	s, err := centralCaseSnapshot(d, opts.Now)
	if err != nil {
		t.Fatal(err)
	}
	call := html.UnescapeString(s.Sections["call"])
	if !strings.Contains(call, "Llamada en curso") || !strings.Contains(call, "Carmen que llego tarde") || !strings.Contains(call, "La frase acordada") {
		t.Errorf("call section: %s", call)
	}
	if strings.Contains(s.Sections["c-head"], "c-situation-line") || s.End || s.State != CaseCalling {
		t.Errorf("snapshot: %+v", s)
	}
	if fam := Build(rec, opts); fam.Call != nil {
		t.Error("the family page now shows the turns during the call")
	}
}

// A cancelled case notified nobody, and says so.
func TestCentralCaseCancelled(t *testing.T) {
	rec := openRecord()
	rec.Calls[0].Verdict, rec.Calls[0].VerdictReason = contract.VerdictNoDuress, "the user said the safe phrase"
	rec.Incident.NotifiedAt = nil
	d := buildCentralCase(rec, CaseExtras{}, Options{Token: testToken, Now: at("2026-10-24T13:11:40Z")})
	s, err := centralCaseSnapshot(d, d.at)
	if err != nil {
		t.Fatal(err)
	}
	if d.Case.State != CaseCancelled || !strings.Contains(s.Sections["c-head"], "Cancelado por la persona") ||
		!strings.Contains(s.Sections["c-family"], "no se avisó a nadie") || strings.Contains(s.Sections["c-family"], "c-notified-summary") {
		t.Errorf("cancelled: %s\n%s", s.Sections["c-head"], s.Sections["c-family"])
	}
}

// The notified panel reads at a glance: a summary line, the relatives by
// role in first-sent order, then the person who asked for help, apart; each
// message in words with its time.
func TestNotifiedPanelSummaryRolesAndStatuses(t *testing.T) {
	ds := []Delivery{
		{Recipient: "+58 424 ···33", Channel: "whatsapp", Status: "read", SentAt: centralNow, ReadAt: ptr(centralNow.Add(4 * time.Second))},
		{Recipient: "+58 414 ···67", Channel: "whatsapp", Status: "delivered", SentAt: centralNow},
		{Recipient: "+58 412 ···77", Channel: "whatsapp", Status: "failed", SentAt: centralNow},
		{Recipient: "+58 424 ···33", Channel: "sms", Status: "accepted", SentAt: centralNow},
		{Recipient: "+58 412 ···77", Channel: "sms", Status: "sent", SentAt: centralNow},
	}
	v := notifiedView(familyChips(ds), "+58 414 ···67")
	if len(v.Family) != 2 || v.Self == nil || v.Family[0].Role != "Familiar 1" || v.Family[0].Recipient != "+58 424 ···33" ||
		v.Family[1].Role != "Familiar 2" || v.Self.Role != "Quien pidió ayuda" || v.Self.Recipient != "+58 414 ···67" {
		t.Fatalf("people = %+v / %+v", v.Family, v.Self)
	}
	f1 := v.Family[0].Sends
	if len(f1) != 2 || f1[0].Channel != "WhatsApp" || f1[0].Label != "Leído" || f1[0].Status != "read" || f1[0].Ticks != "✓✓" ||
		f1[0].Time != clock(centralNow.Add(4*time.Second)) || f1[1].Channel != "SMS" || f1[1].Label != "Enviado" || f1[1].Status != "sent" || f1[1].Ticks != "✓" {
		t.Errorf("relative 1 = %+v", f1)
	}
	if f2 := v.Family[1].Sends; f2[0].Label != "No se pudo enviar" || f2[0].Status != "failed" || f2[1].Label != "Enviado" {
		t.Errorf("relative 2 = %+v", f2)
	}
	if v.Summary != "3 personas avisadas · 1 ya lo leyó" {
		t.Errorf("summary = %q", v.Summary)
	}
	for _, tc := range []struct {
		ds   []Delivery
		want string
	}{
		{[]Delivery{{Recipient: "a", Channel: "whatsapp", Status: "accepted"}}, "1 persona avisada · esperando que llegue"},
		{[]Delivery{{Recipient: "a", Channel: "whatsapp", Status: "delivered"}, {Recipient: "b", Channel: "whatsapp", Status: "delivered"}}, "2 personas avisadas · 2 ya lo recibieron"},
		{[]Delivery{{Recipient: "a", Channel: "whatsapp", Status: "read"}, {Recipient: "b", Channel: "whatsapp", Status: "failed"}}, "1 persona avisada · 1 ya lo leyó · a 1 no se le pudo avisar"},
		{[]Delivery{{Recipient: "a", Channel: "sms", Status: "failed"}}, "No se pudo avisar a nadie"},
	} {
		if got := notifiedView(familyChips(tc.ds), "self").Summary; got != tc.want {
			t.Errorf("summary = %q, want %q", got, tc.want)
		}
	}

	rec := sealedRecord()
	rec.Deliveries = ds
	d := buildCentralCase(rec, CaseExtras{}, Options{Token: testToken, Now: centralNow})
	s, err := centralCaseSnapshot(d, centralNow)
	if err != nil {
		t.Fatal(err)
	}
	fam := html.UnescapeString(s.Sections["c-family"])
	i1, i2, iSelf := strings.Index(fam, "Familiar 1"), strings.Index(fam, "Familiar 2"), strings.Index(fam, "Quien pidió ayuda")
	if !(i1 >= 0 && i1 < i2 && i2 < iSelf) || !strings.Contains(fam, `class="c-person self"`) ||
		!strings.Contains(fam, `<span class="c-channel-name">WhatsApp</span> · <span class="c-send-label">Leído</span>`) ||
		!strings.Contains(fam, "3 personas avisadas · 1 ya lo leyó") || strings.Contains(fam, "quien reporta") {
		t.Errorf("notified panel:\n%s", fam)
	}
}

func TestSituationOf(t *testing.T) {
	for _, raw := range []string{``, `null`, `{}`, `{"lugar": "Petare"}`, `{"situacion": "no se sabe"}`, `[1]`} {
		if got := situationOf(json.RawMessage(raw)); got != "" {
			t.Errorf("situationOf(%s) = %q, want none", raw, got)
		}
	}
	quoted, _ := json.Marshal(testFicha) // stored as a JSON string
	for _, raw := range []json.RawMessage{json.RawMessage(testFicha), quoted, json.RawMessage(`{"Situación": " Parece que se la llevan en un carro. "}`)} {
		if got := situationOf(raw); got != "Parece que se la llevan en un carro." {
			t.Errorf("situationOf(%s) = %q", raw, got)
		}
	}
}

// The central route writes the time next to the points: always the first
// and the newest, the rest only when far enough from every labelled one,
// on alternating sides, inside the box. The family page's route has none.
func TestCentralRouteTimeLabels(t *testing.T) {
	base := at("2026-10-24T13:10:00Z")
	var dense []Point
	for i := 0; i < 40; i++ { // 40 points 25 m apart along a street
		dense = append(dense, Point{Lat: 10.4961 + float64(i)*0.000225, Lng: -66.8531, RecordedAt: base.Add(time.Duration(i) * 15 * time.Second)})
	}
	r := centralRoute(dense)
	if r == nil || len(r.Labels) < 3 || len(r.Labels) >= len(dense)/2 {
		t.Fatalf("%d labels for %d points", len(r.Labels), len(dense))
	}
	first, last := r.Labels[0], r.Labels[len(r.Labels)-1]
	if first.Text != clock(dense[0].RecordedAt) || last.Text != clock(dense[39].RecordedAt) || !last.Newest || first.Newest {
		t.Errorf("first %+v last %+v", first, last)
	}
	pos := func(l RouteLabel) (float64, float64) {
		x, _ := strconv.ParseFloat(l.X, 64)
		y, _ := strconv.ParseFloat(l.Y, 64)
		return x, y
	}
	var sides []float64
	for i, l := range r.Labels {
		x, y := pos(l)
		if x < 0 || x > centralRouteW || y < 0 || y > centralRouteH {
			t.Errorf("label %d outside the box: %v,%v", i, x, y)
		}
		sides = append(sides, x)
		for j := 0; j < i; j++ { // the labelled points are far enough apart
			a, b := labelledDot(t, r, r.Labels[j]), labelledDot(t, r, l)
			if math.Hypot(a.x-b.x, a.y-b.y) < minLabelGap {
				t.Errorf("labels %d and %d sit on points %.1f apart", j, i, math.Hypot(a.x-b.x, a.y-b.y))
			}
		}
	}
	// On a straight vertical path, alternating sides means alternating x.
	for i := 2; i < len(sides); i++ {
		if (sides[i]-sides[i-1])*(sides[i-1]-sides[i-2]) >= 0 {
			t.Errorf("labels %d..%d do not alternate sides: %v", i-2, i, sides[i-2:i+1])
		}
	}
	// No two labels overlap, here or on a dense diagonal route.
	var diagonal []Point
	for i := 0; i < 30; i++ {
		diagonal = append(diagonal, Point{Lat: 10.4961 + float64(i)*0.0003, Lng: -66.8531 + float64(i)*0.0006, RecordedAt: base.Add(time.Duration(i) * 10 * time.Second)})
	}
	for name, rt := range map[string]*Route{"vertical": r, "diagonal": centralRoute(diagonal), "the fixture": centralRoute(sealedRecord().Points)} {
		box := func(l RouteLabel) (x0, x1, y0, y1 float64) {
			x, y := pos(l)
			const w = 54.0
			switch l.Anchor {
			case "start":
				x0, x1 = x, x+w
			case "end":
				x0, x1 = x-w, x
			default:
				x0, x1 = x-w/2, x+w/2
			}
			return x0, x1, y - 11, y + 2
		}
		for _, l := range rt.Labels { // nor sits on the path
			x0, x1, y0, y1 := box(l)
			d := labelledDot(t, rt, l)
		segments:
			for k := 1; k < len(rt.Dots); k++ {
				a, b := rt.Dots[k-1], rt.Dots[k]
				for s := 0.0; s <= 1; s += 0.05 {
					px, py := a.x+(b.x-a.x)*s, a.y+(b.y-a.y)*s
					if math.Hypot(px-d.x, py-d.y) < 6 {
						continue // the label's own point
					}
					if px > x0+1 && px < x1-1 && py > y0+1 && py < y1-1 {
						t.Errorf("%s: the path crosses label %s", name, l.Text)
						break segments
					}
				}
			}
		}
		for i := range rt.Labels {
			for j := 0; j < i; j++ {
				ax0, ax1, ay0, ay1 := box(rt.Labels[i])
				bx0, bx1, by0, by1 := box(rt.Labels[j])
				if ax0 < bx1 && bx0 < ax1 && ay0 < by1 && by0 < ay1 {
					t.Errorf("%s: labels %s and %s overlap", name, rt.Labels[j].Text, rt.Labels[i].Text)
				}
			}
		}
	}
	if fam := route(dense); fam.Labels != nil || fam.Width != 320 || fam.Height != 180 {
		t.Error("the family page's route changed")
	}
	if one := centralRoute(dense[:1]); len(one.Labels) != 1 || !one.Labels[0].Newest {
		t.Errorf("one point: %+v", one.Labels)
	}
}

// labelledDot is the dot a label belongs to (its key starts lbl-<n>-).
func labelledDot(t *testing.T, r *Route, l RouteLabel) RouteDot {
	t.Helper()
	n, err := strconv.Atoi(strings.SplitN(strings.TrimPrefix(l.Key, "lbl-"), "-", 2)[0])
	if err != nil {
		t.Fatal(err)
	}
	return r.Dots[n-1]
}

// The static files the central pages load are embedded.
func TestCentralStaticFiles(t *testing.T) {
	for _, name := range []string{"central.css", "central.js"} {
		rec := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodGet, StaticPrefix+name, nil)
		req.SetPathValue("file", name)
		serveStatic(rec, req)
		if rec.Code != http.StatusOK || rec.Body.Len() < 500 {
			t.Errorf("%s = %d (%d bytes)", name, rec.Code, rec.Body.Len())
		}
	}
}

// main.go gives the server a ReadTimeout (#127). It bounds reading the
// request only (net/http clears the deadline once the request is read), so
// the family page's and the central's streams stay open past it and keep
// getting updates: the projector must not drop and reconnect every minute.
func TestStreamsOutliveTheServerReadTimeout(t *testing.T) {
	src := &liveSource{}
	src.set(testToken, openRecord())
	store := &fakeCentral{}
	store.setCases(caseAt(testIncidentID, centralNow.Add(-time.Minute)))
	h := NewHandler(src, Options{CanSeal: true, Channel: "WhatsApp"}).WithCentral(&Central{Key: testCentralKey, Store: store})
	h.LivePoll, h.KeepAlive = 10*time.Millisecond, time.Hour
	h.Now = func() time.Time { return centralNow }
	mux := http.NewServeMux()
	h.Register(mux)
	srv := httptest.NewUnstartedServer(mux)
	srv.Config.ReadTimeout = 150 * time.Millisecond
	srv.Start()
	t.Cleanup(srv.Close) // after the streams below close (cleanups run last first)
	ck := login(t, srv)
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)

	open := func(path string) chan sseMessage {
		req, _ := http.NewRequestWithContext(ctx, http.MethodGet, srv.URL+path, nil)
		req.AddCookie(ck)
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { resp.Body.Close() })
		msgs := make(chan sseMessage, 16)
		go readSSE(bufio.NewReader(resp.Body), msgs)
		if m := next(t, msgs); m.event != "update" {
			t.Fatalf("%s: first message %+v", path, m)
		}
		return msgs
	}
	family, list := open("/i/"+testToken+"/live"), open("/central/live")
	time.Sleep(450 * time.Millisecond) // three read timeouts

	rec := openRecord()
	rec.Incident.NotifiedAt = nil
	src.set(testToken, rec)
	if m := next(t, family); m.event != "update" {
		t.Errorf("family stream after the read timeout: %+v", m)
	}
	store.setCases(caseAt(testIncidentID, centralNow.Add(-time.Minute)), caseAt(cancelledID, centralNow.Add(-2*time.Minute)))
	if m := next(t, list); m.event != "update" {
		t.Errorf("central stream after the read timeout: %+v", m)
	}
}

// The list is not per case: it stays open even when every case is closed.
// The detail follows a sealed case for a while (receipts come after the
// seal) and then ends, like the family page.
func TestCentralStreamsEndOnTheirOwnRules(t *testing.T) {
	store := &fakeCentral{}
	old := caseAt(cancelledID, centralNow.Add(-3*time.Hour))
	old.SealedAt = ptr(centralNow.Add(-3 * time.Hour))
	store.setCases(old)
	store.setCase(sealedRecord(), CaseExtras{}) // sealed at 13:11:25
	var now atomic.Int64
	now.Store(at("2026-10-24T13:20:00Z").UnixNano()) // 9 min after the seal
	h := NewHandler(fakeSource{}, Options{CanSeal: true, Channel: "WhatsApp"}).WithCentral(&Central{Key: testCentralKey, Store: store})
	h.LivePoll, h.KeepAlive = 10*time.Millisecond, 20*time.Millisecond
	h.Now = func() time.Time { return time.Unix(0, now.Load()) }
	mux := http.NewServeMux()
	h.Register(mux)
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	ck := login(t, srv)
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	open := func(path string) chan sseMessage {
		req, _ := http.NewRequestWithContext(ctx, http.MethodGet, srv.URL+path, nil)
		req.AddCookie(ck)
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { resp.Body.Close() })
		msgs := make(chan sseMessage, 16)
		go readSSE(bufio.NewReader(resp.Body), msgs)
		return msgs
	}
	// quiet reads for d and fails on anything but keep-alives.
	quiet := func(name string, msgs <-chan sseMessage, d time.Duration) {
		t.Helper()
		deadline := time.After(d)
		for {
			select {
			case m := <-msgs:
				if m.comment != "keep-alive" {
					t.Fatalf("%s: %+v, want only keep-alives", name, m)
				}
			case <-deadline:
				return
			}
		}
	}

	list := open("/central/live")
	if s := decodeAny(t, next(t, list)); !s.Live || s.Over() {
		t.Fatalf("list: live %t over %t", s.Live, s.Over())
	}
	detail := open("/central/r/" + testIncidentID + "/live")
	if s := decodeAny(t, next(t, detail)); !s.Live || s.Over() || !strings.Contains(s.Sections["status"], "Sellada") {
		t.Fatalf("detail 9 min after the seal: live %t over %t", s.Live, s.Over())
	}
	quiet("list", list, 120*time.Millisecond)
	quiet("detail", detail, 60*time.Millisecond)

	now.Store(at("2026-10-24T14:00:00Z").UnixNano()) // 48 min after the seal
	m := next(t, detail)
	if m.event == "update" { // only when a section changed; here none does
		if s := decodeAny(t, m); s.Live {
			t.Fatal("detail long after the seal is still live")
		}
		m = next(t, detail)
	}
	if m.event != "end" {
		t.Fatalf("detail long after the seal: %+v, want end", m)
	}
	quiet("list after the detail ended", list, 120*time.Millisecond)
}
