package report

import (
	"context"
	"errors"
	"html"
	"io/fs"
	"net/http"
	"net/http/httptest"
	"regexp"
	"strings"
	"testing"
	"time"
)

// fakeSource serves records by token from memory.
type fakeSource map[string]Record

func (f fakeSource) ByToken(_ context.Context, token string) (Record, error) {
	if rec, ok := f[token]; ok {
		return rec, nil
	}
	return Record{}, ErrNotFound
}

// Audio serves "fake mp3" (the fixture's recording) for a call that has one.
func (f fakeSource) Audio(_ context.Context, token string, n int) ([]byte, Recording, error) {
	rec, ok := f[token]
	if !ok {
		return nil, Recording{}, ErrNotFound
	}
	for i := len(rec.Calls) - 1; i >= 0; i-- {
		if r := rec.Calls[i].Recording; r != nil && (n == 0 || n == i+1) {
			return []byte("fake mp3"), *r, nil
		}
	}
	return nil, Recording{}, ErrNotFound
}

type failingSource struct{}

func (failingSource) ByToken(context.Context, string) (Record, error) {
	return Record{}, errors.New("db down")
}

func (failingSource) Audio(context.Context, string, int) ([]byte, Recording, error) {
	return nil, Recording{}, errors.New("db down")
}

func serve(t *testing.T, src Source, path string) *httptest.ResponseRecorder {
	t.Helper()
	mux := http.NewServeMux()
	h := NewHandler(src, Options{CanSeal: true, Channel: "WhatsApp"})
	h.Now = func() time.Time { return at("2026-10-24T13:11:40Z") } // the fixture's case is live
	h.Register(mux)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, path, nil))
	return rec
}

func checkPrivacyHeaders(t *testing.T, rec *httptest.ResponseRecorder) {
	t.Helper()
	h := rec.Header()
	if h.Get("Referrer-Policy") != "no-referrer" || !strings.Contains(h.Get("X-Robots-Tag"), "noindex") || h.Get("Cache-Control") != "no-store" {
		t.Errorf("privacy headers: Referrer-Policy=%q X-Robots-Tag=%q Cache-Control=%q",
			h.Get("Referrer-Policy"), h.Get("X-Robots-Tag"), h.Get("Cache-Control"))
	}
	// Only the map's tile images may come from elsewhere
	// (TestCSPAllowsOnlyTheMapTiles).
	if csp := h.Get("Content-Security-Policy"); csp != contentSecurityPolicy || !strings.Contains(csp, "default-src 'none'") {
		t.Errorf("CSP = %q, want contentSecurityPolicy", csp)
	}
}

func TestPageRendersTheSealedReport(t *testing.T) {
	rec := serve(t, fakeSource{testToken: sealedRecord()}, "/i/"+testToken)
	if rec.Code != http.StatusOK || rec.Header().Get("Content-Type") != "text/html; charset=utf-8" {
		t.Fatalf("GET = %d %q", rec.Code, rec.Header().Get("Content-Type"))
	}
	checkPrivacyHeaders(t, rec)
	body := html.UnescapeString(rec.Body.String())
	for _, want := range []string{
		"Sellada ✓", ">Coacción<", "mama_lista.webp", "Pidió ayuda · sáb 24 oct, 09:10", "duró",
		`class="node node-done" data-stage="stage-trigger"`, `class="node node-done node-alarm" data-stage="stage-verdict"`,
		`class="node node-done" data-stage="stage-family"`, `class="node node-done" data-stage="stage-sealed"`,
		`class="node node-skipped" data-stage="stage-rang"`, ">09:11:25<", "Familia avisada",
		`<audio id="audio-1" data-call="1" preload="none" src="/i/` + testToken + `/audio.mp3?llamada=1">`,
		`data-play="1"`, `data-seek="15"`, "La frase acordada", "Tu familiar", "Coacción: dijo la frase acordada",
		`class="seg" pathLength="1"`, `class="dot newest"`, "recorridos", "Abrir en Maps",
		`href="/i/` + testToken + `/reporte.pdf"`, "Descargar PDF", "WhatsApp enviado a 2 familiares.",
	} {
		if !strings.Contains(body, want) {
			t.Errorf("page lacks %q", want)
		}
	}
	// The redesign leaves these to the PDF, or drops them.
	for _, gone := range []string{"Caminando (4 km/h)", "SHA-256", "El sello prueba", "Toca un turno", "<footer", "lat-value", "En vivo"} {
		if strings.Contains(body, gone) {
			t.Errorf("page still has %q", gone)
		}
	}
	// The full phone number and the device id never reach the page.
	for _, leak := range []string{"+584141234567", "4141234567", sealedRecord().Incident.DeviceID} {
		if strings.Contains(body, leak) {
			t.Errorf("page leaks %q", leak)
		}
	}
}

func TestPageWithoutAudioSaysSoAndKeepsTheTranscript(t *testing.T) {
	body := html.UnescapeString(serve(t, fakeSource{testToken: openRecord()}, "/i/"+testToken).Body.String())
	for _, want := range []string{"En vivo", "mama_escuchando.webp", "Guardando la grabación de la llamada…", "dile a la tía Carmen que llego tarde",
		`class="node node-current" data-stage="stage-sealed"`, "desde el gesto", "Ver toda la llamada", `class="user keep"`} {
		if !strings.Contains(body, want) {
			t.Errorf("page lacks %q", want)
		}
	}
	if strings.Contains(body, "<audio") || strings.Contains(body, "data-play") {
		t.Error("page has a player without a recording")
	}
}

// Unknown, malformed and expired tokens all look the same: 404.
func TestUnknownTokenIs404(t *testing.T) {
	for _, path := range []string{"/i/BBBBBBBBBBBBBBBBBBBBBB", "/i/" + testIncidentID, "/i/x"} {
		rec := serve(t, fakeSource{testToken: sealedRecord()}, path)
		if rec.Code != http.StatusNotFound || !strings.Contains(rec.Body.String(), "no existe o ya venció") {
			t.Errorf("GET %s = %d", path, rec.Code)
		}
		checkPrivacyHeaders(t, rec)
	}
}

func TestStoreFailureIs500WithoutDetails(t *testing.T) {
	rec := serve(t, failingSource{}, "/i/"+testToken)
	if rec.Code != http.StatusInternalServerError || strings.Contains(rec.Body.String(), "db down") {
		t.Errorf("GET = %d %q", rec.Code, rec.Body.String())
	}
}

var urlAttr = regexp.MustCompile(`(?:src|href|action|poster|data)="([^"]*)"`)

// Opening a report makes no request outside this server but the map's
// tiles: every src and href on the page is a path on this origin, except the
// "Abrir en Maps" links (navigations the reader chooses, never loaded by the
// page), and the page's own CSS and JS load nothing either. route-map.js
// asks the tile server alone (TestRouteMapRequestsOnlyTiles).
func TestPageMakesNoThirdPartyRequest(t *testing.T) {
	body := serve(t, fakeSource{testToken: sealedRecord()}, "/i/"+testToken).Body.String()
	for _, m := range urlAttr.FindAllStringSubmatch(body, -1) {
		u := m[1]
		if strings.HasPrefix(u, "/") && !strings.HasPrefix(u, "//") {
			continue
		}
		if strings.HasPrefix(u, "https://www.google.com/maps/search/") && strings.Contains(body, `class="maps-btn" href="`+u+`"`) {
			continue
		}
		t.Errorf("page references %q", u)
	}
	for _, name := range []string{"static/ficha.css", "static/ficha.js"} {
		b, err := fs.ReadFile(staticFS, name)
		if err != nil {
			t.Fatal(err)
		}
		// The script fetches only URLs the page gives it (the live stream,
		// estado.json, the recording: all paths on this server); the CSP's
		// connect-src 'self' enforces it in the browser.
		for _, m := range regexp.MustCompile(`url\(([^)]*)\)|https?:|["']//|XMLHttpRequest|@import|WebSocket|sendBeacon`).FindAllString(string(b), -1) {
			if !strings.HasPrefix(m, `url("/ficha/static/`) {
				t.Errorf("%s: %q", name, m)
			}
		}
	}
}

func TestStaticFiles(t *testing.T) {
	for path, typ := range map[string]string{
		"/ficha/static/ficha.css":            "text/css; charset=utf-8",
		"/ficha/static/ficha.js":             "text/javascript; charset=utf-8",
		"/ficha/static/lexend_regular.ttf":   "font/ttf",
		"/ficha/static/lexend_medium.ttf":    "font/ttf",
		"/ficha/static/lexend_bold.ttf":      "font/ttf",
		"/ficha/static/mama_escuchando.webp": "image/webp",
		"/ficha/static/mama_lista.webp":      "image/webp",
		"/ficha/static/mama_llamando.webp":   "image/webp",
		"/ficha/static/Lexend-OFL.txt":       "text/plain; charset=utf-8",
		"/ficha/static/mama_mark.webp":       "image/webp",
	} {
		rec := serve(t, fakeSource{}, path)
		if rec.Code != http.StatusOK || rec.Header().Get("Content-Type") != typ || rec.Body.Len() == 0 {
			t.Errorf("GET %s = %d %q (%d bytes)", path, rec.Code, rec.Header().Get("Content-Type"), rec.Body.Len())
		}
	}
	for _, path := range []string{"/ficha/static/nope.css", "/ficha/static/ficha.html", "/ficha/static/..%2Fpage.go"} {
		if rec := serve(t, fakeSource{}, path); rec.Code != http.StatusNotFound {
			t.Errorf("GET %s = %d, want 404", path, rec.Code)
		}
	}
}

func TestPageURL(t *testing.T) {
	if got := PageURL("https://mama.example.com/", testToken); got != "https://mama.example.com/i/"+testToken {
		t.Errorf("PageURL = %q", got)
	}
	if got := (Link{Token: testToken}).URL(""); got != "/i/"+testToken {
		t.Errorf("URL(\"\") = %q", got)
	}
}

func TestAudioIsServedWithRangesAndPrivacyHeaders(t *testing.T) {
	src := fakeSource{testToken: sealedRecord()}
	rec := serve(t, src, "/i/"+testToken+"/audio.mp3?llamada=1")
	if rec.Code != http.StatusOK || rec.Body.String() != "fake mp3" || rec.Header().Get("Content-Type") != "audio/mpeg" || rec.Header().Get("Accept-Ranges") != "bytes" {
		t.Fatalf("GET audio = %d %q %q ranges=%q", rec.Code, rec.Body.String(), rec.Header().Get("Content-Type"), rec.Header().Get("Accept-Ranges"))
	}
	checkPrivacyHeaders(t, rec)

	mux := http.NewServeMux()
	NewHandler(src, Options{}).Register(mux)
	req := httptest.NewRequest(http.MethodGet, "/i/"+testToken+"/audio.mp3", nil)
	req.Header.Set("Range", "bytes=5-")
	rr := httptest.NewRecorder()
	mux.ServeHTTP(rr, req)
	if rr.Code != http.StatusPartialContent || rr.Body.String() != "mp3" {
		t.Errorf("GET audio Range 5- = %d %q, want 206 \"mp3\"", rr.Code, rr.Body.String())
	}

	for _, path := range []string{
		"/i/" + testToken + "/audio.mp3?llamada=2",
		"/i/" + testToken + "/audio.mp3?llamada=0",
		"/i/" + testToken + "/audio.mp3?llamada=x",
		"/i/BBBBBBBBBBBBBBBBBBBBBB/audio.mp3",
	} {
		if rec := serve(t, src, path); rec.Code != http.StatusNotFound {
			t.Errorf("GET %s = %d, want 404", path, rec.Code)
		}
	}
	if rec := serve(t, fakeSource{testToken: openRecord()}, "/i/"+testToken+"/audio.mp3"); rec.Code != http.StatusNotFound {
		t.Errorf("GET audio without a recording = %d, want 404", rec.Code)
	}
	if rec := serve(t, failingSource{}, "/i/"+testToken+"/audio.mp3"); rec.Code != http.StatusInternalServerError {
		t.Errorf("GET audio with the store down = %d, want 500", rec.Code)
	}
}

// Every pose Build can pick is embedded.
func TestEveryPoseIsEmbedded(t *testing.T) {
	for _, pose := range []string{"escuchando", "llamando", "lista"} {
		if _, err := fs.ReadFile(staticFS, "static/mama_"+pose+".webp"); err != nil {
			t.Errorf("pose %s: %v", pose, err)
		}
	}
}

// The ficha and the central of one case often sit in two tabs: ficha.js
// plays one recording at a time, pausing the others on the page and, over a
// BroadcastChannel, in the reader's other tabs of this server. A headless
// check of two tabs is in the PR; this keeps the guard from being dropped.
func TestFichaJSPlaysOneRecordingAtATime(t *testing.T) {
	b, err := fs.ReadFile(staticFS, "static/ficha.js")
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{`new BroadcastChannel("mama-audio")`, `document.addEventListener("play", function (ev) {`, "pauseOthers(ev.target)", "pauseOthers(null)"} {
		if !strings.Contains(string(b), want) {
			t.Errorf("ficha.js lacks %q", want)
		}
	}
}
