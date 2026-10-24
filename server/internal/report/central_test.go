package report

import (
	"bufio"
	"context"
	"encoding/json"
	"html"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync"
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
// and #104's name.
func TestCentralCaseWithAndWithoutContextAndName(t *testing.T) {
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
		"Esta llamada no dejó contexto.",              // no ficha
		"/i/" + testToken + "/audio.mp3?llamada=1",    // the recording, through the report link
		"La frase acordada", "Recibido 0,4 s después", // transcript and timeline
		"Abrir en Maps", "10.49890, -66.84770", // route and points
		"/i/" + testToken + "/reporte.pdf",
	} {
		if !strings.Contains(text, want) {
			t.Errorf("detail lacks %q", want)
		}
	}
	if resp.StatusCode != http.StatusOK || strings.Contains(text, "+584141234567") || strings.Contains(text, rec.Incident.DeviceID) {
		t.Errorf("detail = %d, or it shows the full number or the device id", resp.StatusCode)
	}

	rec.Device.UserName = "María Rodríguez"
	store.setCase(rec, CaseExtras{Ficha: json.RawMessage(testFicha)})
	_, body = cget(t, srv.URL+"/central/r/"+testIncidentID, ck)
	text = html.UnescapeString(body)
	for _, want := range []string{"María Rodríguez", "<title>María Rodríguez · Central · MAMÁ</title>",
		"Parece que se la llevan en un carro.", "Autopista Caracas-La Guaira", "Dos hombres", "Carro gris",
		`c-field unknown"><dt>Rumbo</dt><dd>No se sabe`, "Confianza alta"} {
		if !strings.Contains(text, want) {
			t.Errorf("detail with ficha and name lacks %q", want)
		}
	}
	if strings.Contains(text, "Sin nombre registrado") || strings.Contains(text, "no dejó contexto") {
		t.Error("detail still shows the empty states")
	}
}

// While the call is on, the detail shows the turns stored so far (#97
// appends them live), the phrase marked as soon as it is said, and the
// context waiting; the family page keeps them for the end of the call.
func TestCentralCaseShowsTurnsDuringTheCall(t *testing.T) {
	rec := openRecord()
	c := &rec.Calls[0]
	c.Status, c.EndedAt, c.DurationSecs, c.Verdict, c.VerdictAt, c.VerdictReason = "in_progress", nil, nil, "", nil, ""
	c.Transcript = c.Transcript[:4]
	rec.Incident.NotifiedAt = nil
	rec.Points = rec.Points[:2]
	opts := Options{Token: testToken, Channel: "WhatsApp", CanSeal: true, Now: at("2026-10-24T13:10:50Z")}
	d := buildCentralCase(rec, CaseExtras{}, opts)
	if d.Case.State != CaseCalling || d.Call == nil || len(d.Call.Turns) != 4 || !d.Call.Turns[3].Phrase || !d.ContextWait {
		t.Fatalf("state %s call %+v wait %t", d.Case.State, d.Call, d.ContextWait)
	}
	s, err := centralCaseSnapshot(d, opts.Now)
	if err != nil {
		t.Fatal(err)
	}
	call := html.UnescapeString(s.Sections["call"])
	if !strings.Contains(call, "Llamada en curso") || !strings.Contains(call, "Carmen que llego tarde") || !strings.Contains(call, "La frase acordada") {
		t.Errorf("call section: %s", call)
	}
	if !strings.Contains(s.Sections["c-context"], "fields-wait") || s.End || s.State != CaseCalling {
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
		!strings.Contains(s.Sections["c-family"], "no se avisó a nadie") || !strings.Contains(s.Sections["c-context"], "dijo que está bien") {
		t.Errorf("cancelled: %s\n%s\n%s", s.Sections["c-head"], s.Sections["c-family"], s.Sections["c-context"])
	}
}

func TestNotifiedPeopleGroupsByNumber(t *testing.T) {
	ds := []Delivery{
		{Recipient: "+58 424 ···33", Channel: "whatsapp", Status: "read", SentAt: centralNow, ReadAt: ptr(centralNow)},
		{Recipient: "+58 412 ···77", Channel: "whatsapp", Status: "delivered", SentAt: centralNow},
		{Recipient: "+58 424 ···33", Channel: "sms", Status: "accepted", SentAt: centralNow},
		{Recipient: "+58 414 ···67", Channel: "whatsapp", Status: "failed", SentAt: centralNow},
	}
	p := notifiedPeople(familyChips(ds), "+58 414 ···67")
	if len(p) != 3 || len(p[0].Sends) != 2 || p[0].Sends[1].Channel != "SMS" || p[0].Sends[0].Label != "leído" || !p[2].Self || p[0].Self {
		t.Errorf("people = %+v", p)
	}
}

func TestContextView(t *testing.T) {
	if contextView(nil) != nil || contextView(json.RawMessage(`null`)) != nil || contextView(json.RawMessage(`{}`)) != nil {
		t.Error("an empty ficha made a card")
	}
	quoted, _ := json.Marshal(testFicha) // stored as a JSON string
	for _, raw := range []json.RawMessage{json.RawMessage(testFicha), quoted} {
		v := contextView(raw)
		if v == nil || v.Situation == "" || v.Confidence != "alta" || len(v.Fields) != 4 || v.Fields[2].Value != "Carro gris" || !v.Fields[3].Unknown {
			t.Errorf("ficha %s = %+v", raw, v)
		}
	}
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
