package report

import (
	"bytes"
	"net/http"
	"net/http/httptest"
	"regexp"
	"strings"
	"testing"
	"testing/fstest"
	"time"
)

func TestContentVersionChangesWithTheBytes(t *testing.T) {
	a, b := contentVersion([]byte("body { color: red }")), contentVersion([]byte("body { color: blue }"))
	if a == b || len(a) != 10 || !regexp.MustCompile(`^[0-9a-f]{10}$`).MatchString(a) {
		t.Errorf("versions %q and %q", a, b)
	}
	if a != contentVersion([]byte("body { color: red }")) {
		t.Error("the same bytes gave another version")
	}
}

// A CSS file's url()s to static files carry their versions, so a new font
// also changes the version of the CSS that loads it.
func TestCSSVersionFollowsTheFilesItLoads(t *testing.T) {
	css := `@font-face { src: url("/ficha/static/f.ttf") format("truetype"); } .x { background: url(/ficha/static/a.webp); } .y { background: url("/ficha/static/gone.webp"); }`
	fsys := fstest.MapFS{
		"s/site.css": {Data: []byte(css)},
		"s/f.ttf":    {Data: []byte("font v1")},
		"s/a.webp":   {Data: []byte("image")},
		"s/notes.md": {Data: []byte("not served")},
	}
	one := loadAssets(fsys, "s")
	got := string(one["site.css"].body)
	if !strings.Contains(got, `url("/ficha/static/f.ttf?v=`+one["f.ttf"].version+`")`) ||
		!strings.Contains(got, `url(/ficha/static/a.webp?v=`+one["a.webp"].version+`)`) ||
		!strings.Contains(got, `url("/ficha/static/gone.webp")`) {
		t.Errorf("rewritten CSS: %s", got)
	}
	if _, ok := one["notes.md"]; ok {
		t.Error("a file of an unserved type was loaded")
	}
	fsys["s/f.ttf"] = &fstest.MapFile{Data: []byte("font v2")}
	two := loadAssets(fsys, "s")
	if two["f.ttf"].version == one["f.ttf"].version || two["site.css"].version == one["site.css"].version {
		t.Error("a new font did not change the font's and the CSS's versions")
	}
	if two["a.webp"].version != one["a.webp"].version {
		t.Error("an unchanged file changed version")
	}
	if served := string(assets["ficha.css"].body); !strings.Contains(served, "lexend_regular.ttf?v="+assets["lexend_regular.ttf"].version) {
		t.Error("the served ficha.css loads its font without the version")
	}
}

// staticRef finds every static URL in a page, with its version if any.
var staticRef = regexp.MustCompile(regexp.QuoteMeta(StaticPrefix) + `([A-Za-z0-9._-]+)(\?v=([0-9a-f]+))?`)

// Every static URL the family and central pages emit carries the current
// version of its file.
func TestEveryPageLinksVersionedStaticFiles(t *testing.T) {
	src := fakeSource{testToken: sealedRecord()}
	store := &fakeCentral{}
	store.setCases(caseAt(testIncidentID, centralNow.Add(-time.Minute)))
	store.setCase(openRecord(), CaseExtras{})
	srv := centralServer(t, store, testCentralKey)
	ck := login(t, srv)
	pages := map[string]string{
		"family page":  serve(t, src, "/i/"+testToken).Body.String(),
		"missing page": serve(t, src, "/i/BBBBBBBBBBBBBBBBBBBBBB").Body.String(),
	}
	var buf bytes.Buffer
	if err := tmpl.ExecuteTemplate(&buf, "demo-wait", "/demo/next.json"); err != nil {
		t.Fatal(err)
	}
	pages["demo wait page"] = buf.String()
	_, pages["central list"] = cget(t, srv.URL+"/central/", ck)
	_, pages["central case"] = cget(t, srv.URL+"/central/r/"+testIncidentID, ck)
	store.setCases()
	_, pages["empty central list"] = cget(t, srv.URL+"/central/", ck)

	for name, html := range pages {
		refs := staticRef.FindAllStringSubmatch(html, -1)
		if len(refs) == 0 {
			t.Errorf("%s links no static file", name)
		}
		for _, r := range refs {
			a, ok := assets[r[1]]
			if !ok || r[3] != a.version {
				t.Errorf("%s: %s has version %q, want %q", name, r[0], r[3], a.version)
			}
		}
	}
	if !strings.Contains(pages["central list"], "ficha.css?v=") || !strings.Contains(pages["central case"], "central.js?v=") ||
		!strings.Contains(pages["family page"], "mama_lista.webp?v=") {
		t.Error("a page lost its stylesheet, script or art")
	}
}

// With the current version a file may be cached for good; without it, or
// with an old one, it is revalidated against its ETag.
func TestStaticCaching(t *testing.T) {
	a := assets["ficha.css"]
	get := func(target, ifNoneMatch string) *httptest.ResponseRecorder {
		req := httptest.NewRequest(http.MethodGet, target, nil)
		req.SetPathValue("file", "ficha.css")
		if ifNoneMatch != "" {
			req.Header.Set("If-None-Match", ifNoneMatch)
		}
		rec := httptest.NewRecorder()
		serveStatic(rec, req)
		return rec
	}
	if rec := get(StaticURL("ficha.css"), ""); rec.Code != http.StatusOK || rec.Header().Get("Cache-Control") != cacheVersioned ||
		!strings.Contains(rec.Header().Get("Cache-Control"), "immutable") || rec.Body.String() != string(a.body) {
		t.Errorf("versioned: %d %q", rec.Code, rec.Header().Get("Cache-Control"))
	}
	for _, target := range []string{StaticPrefix + "ficha.css", StaticPrefix + "ficha.css?v=0000000000"} {
		rec := get(target, "")
		if rec.Code != http.StatusOK || rec.Header().Get("Cache-Control") != "no-cache" || rec.Header().Get("ETag") != `"`+a.version+`"` {
			t.Errorf("%s: %d %q etag %q", target, rec.Code, rec.Header().Get("Cache-Control"), rec.Header().Get("ETag"))
		}
		if rec.Header().Get("Referrer-Policy") != "no-referrer" || rec.Header().Get("Content-Type") != "text/css; charset=utf-8" {
			t.Errorf("%s: headers %v", target, rec.Header())
		}
	}
	if rec := get(StaticPrefix+"ficha.css", `"`+a.version+`"`); rec.Code != http.StatusNotModified || rec.Body.Len() != 0 {
		t.Errorf("revalidation with the current ETag = %d (%d bytes), want 304", rec.Code, rec.Body.Len())
	}
	if rec := get(StaticPrefix+"ficha.css", `"0000000000"`); rec.Code != http.StatusOK {
		t.Errorf("revalidation with an old ETag = %d, want 200", rec.Code)
	}
	if StaticURL("nope.css") != StaticPrefix+"nope.css" {
		t.Error("an unknown file got a version")
	}
}
