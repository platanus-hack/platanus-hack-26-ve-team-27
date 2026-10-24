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
	NewHandler(src, Options{CanSeal: true, Channel: "WhatsApp"}).Register(mux)
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
	if csp := h.Get("Content-Security-Policy"); !strings.Contains(csp, "default-src 'none'") || strings.Contains(csp, "http") {
		t.Errorf("CSP = %q, want only 'self'", csp)
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
		"Cerrado y sellado", ">Coacción<", "mama_lista.webp",
		"Pidió ayuda con el gesto de volumen", "Recibido 0,4 s después", "Caminando (4 km/h): Mamá llama",
		"Llamando a +58 ···67", "Llamada de 1 min 1 s", "Coacción: dijo la frase acordada",
		"WhatsApp enviado a 2 familiares", "Grabación guardada", "Ficha sellada",
		`<audio id="audio-1" data-call="1" controls preload="none" src="/i/` + testToken + `/audio.mp3?llamada=1">`,
		`data-seek="15"`, "La frase acordada", "Tu familiar", "Mamá",
		`class="clock clock-phone"`, `class="clock clock-server"`, `class="clock clock-elevenlabs"`,
		"Abrir en Maps", "±12 m", `class="seg" pathLength="1"`, `class="dot newest"`,
		`href="/i/` + testToken + `/ficha.json"`, `href="/i/` + testToken + `/ficha.sig"`,
		"El sello prueba que el registro no cambió desde que el servidor de MAMÁ lo cerró. No es una firma electrónica certificada ni reemplaza una experticia.",
	} {
		if !strings.Contains(body, want) {
			t.Errorf("page lacks %q", want)
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
	for _, want := range []string{"Abierto", "mama_escuchando.webp", "Audio no disponible", "dile a la tía Carmen que llego tarde", "Sin sellar", "Toca"} {
		if want == "Toca" {
			if strings.Contains(body, "Toca un turno") {
				t.Error("page offers tap-to-seek without audio")
			}
			continue
		}
		if !strings.Contains(body, want) {
			t.Errorf("page lacks %q", want)
		}
	}
	if strings.Contains(body, "<audio") {
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

// Opening a report makes no request outside this server: every src and
// href on the page is a path on this origin, except the "Abrir en Maps"
// links (navigations the reader chooses, never loaded by the page), and the
// CSS and JS load nothing either.
func TestPageMakesNoThirdPartyRequest(t *testing.T) {
	body := serve(t, fakeSource{testToken: sealedRecord()}, "/i/"+testToken).Body.String()
	for _, m := range urlAttr.FindAllStringSubmatch(body, -1) {
		u := m[1]
		if strings.HasPrefix(u, "/") && !strings.HasPrefix(u, "//") {
			continue
		}
		if strings.HasPrefix(u, "https://www.google.com/maps/search/") && strings.Contains(body, `<a class="maps" href="`+u+`"`) {
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
