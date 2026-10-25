package report

import (
	"bytes"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"html"
	"io/fs"
	"math"
	"net/http"
	"net/http/httptest"
	"regexp"
	"strings"
	"testing"
	"time"
)

// The map's points are the drawing's, on their real coordinates, with the
// same tooltip; the accuracy is left out when the phone sent none.
func TestRouteMapCarriesThePoints(t *testing.T) {
	pts := sealedRecord().Points
	r := route(pts)
	if len(r.Map) != len(pts) {
		t.Fatalf("%d map points for %d points", len(r.Map), len(pts))
	}
	for i, m := range r.Map {
		if m.Lat != pts[i].Lat || m.Lng != pts[i].Lng || m.Title != r.Dots[i].Title || m.Label != "" ||
			m.Acc == nil || *m.Acc != *pts[i].AccuracyM {
			t.Errorf("map point %d = %+v, want point %+v with title %q", i, m, pts[i], r.Dots[i].Title)
		}
	}
	var got []map[string]any
	if err := json.Unmarshal([]byte(r.MapJSON()), &got); err != nil || len(got) != 3 {
		t.Fatalf("MapJSON = %s (%v)", r.MapJSON(), err)
	}
	for _, k := range []string{"lat", "lng", "acc", "title"} {
		if _, ok := got[0][k]; !ok {
			t.Errorf("MapJSON's point lacks %q: %v", k, got[0])
		}
	}
	if _, ok := got[0]["label"]; ok {
		t.Errorf("the family page's point has a label: %v", got[0])
	}

	noAcc := route([]Point{{Lat: 10.49, Lng: -66.85}, {Lat: 10.491, Lng: -66.851, AccuracyM: ptr(math.Inf(1))}})
	if j := noAcc.MapJSON(); strings.Contains(j, "acc") {
		t.Errorf("MapJSON without a usable accuracy = %s", j)
	}
	if bad := route([]Point{{Lat: math.NaN(), Lng: -66.85}}); bad.MapJSON() != "[]" {
		t.Errorf("MapJSON with a NaN = %s, want []", bad.MapJSON())
	}
}

// On the central the map writes the same times as its drawing, next to the
// same points.
func TestCentralRouteMapLabels(t *testing.T) {
	base := at("2026-10-24T13:10:00Z")
	var dense []Point
	for i := 0; i < 40; i++ {
		dense = append(dense, Point{Lat: 10.4961 + float64(i)*0.000225, Lng: -66.8531, RecordedAt: base.Add(time.Duration(i) * 15 * time.Second)})
	}
	r := centralRoute(dense)
	labelled := map[string]bool{}
	for _, l := range r.Labels {
		labelled[labelledDot(t, r, l).Key] = true
	}
	n := 0
	for i, m := range r.Map {
		if m.Label == "" {
			continue
		}
		n++
		if m.Label != clock(dense[i].RecordedAt) || !labelled[r.Dots[i].Key] {
			t.Errorf("map point %d labelled %q, drawing labels %v", i, m.Label, labelled[r.Dots[i].Key])
		}
	}
	if n != len(r.Labels) || r.Map[len(r.Map)-1].Label == "" {
		t.Errorf("%d map labels for %d drawn ones; newest %q", n, len(r.Labels), r.Map[len(r.Map)-1].Label)
	}
}

var (
	dataPoints = regexp.MustCompile(`data-points="([^"]*)"`)
	mapDiv     = regexp.MustCompile(`<div data-key="route-map"[^>]*></div>`)
)

// pagePoints is the route box's data-points on a rendered page.
func pagePoints(t *testing.T, name, body string) []MapPoint {
	t.Helper()
	m := dataPoints.FindAllStringSubmatch(body, -1)
	if len(m) != 1 {
		t.Fatalf("%s has %d data-points", name, len(m))
	}
	var pts []MapPoint
	if err := json.Unmarshal([]byte(html.UnescapeString(m[0][1])), &pts); err != nil {
		t.Fatalf("%s: data-points %q: %v", name, m[0][1], err)
	}
	return pts
}

// The family page and the central's case hand the points to the map, and
// keep the drawing for when there is no map.
func TestRouteSectionHandsThePointsToTheMap(t *testing.T) {
	src := fakeSource{testToken: sealedRecord()}
	family := serve(t, src, "/i/"+testToken).Body.String()

	store := &fakeCentral{}
	store.setCases(caseAt(testIncidentID, centralNow.Add(-time.Minute)))
	store.setCase(openRecord(), CaseExtras{})
	srv := centralServer(t, store, testCentralKey)
	_, central := cget(t, srv.URL+"/central/r/"+testIncidentID, login(t, srv))

	for name, body := range map[string]string{"family page": family, "central case": central} {
		pts := pagePoints(t, name, body)
		if len(pts) == 0 || pts[0].Lat != 10.4961 || pts[0].Lng != -66.8531 || pts[0].Title == "" {
			t.Errorf("%s: points %+v", name, pts)
		}
		if len(mapDiv.FindAllString(body, -1)) != 1 || !strings.Contains(body, `class="route" viewBox=`) {
			t.Errorf("%s: want one map div and the drawn route", name)
		}
		for _, f := range []string{"leaflet.css?v=", "leaflet.js?v=", "route-map.js?v="} {
			if !strings.Contains(body, f) {
				t.Errorf("%s does not load %s", name, f)
			}
		}
	}
	if pts := pagePoints(t, "central case", central); pts[len(pts)-1].Label == "" {
		t.Error("the central's newest point has no time on the map")
	}
}

// The map div is the same HTML whatever the points, so the live reconcile
// keeps it, and the map mounted in it, through every update; the points
// change in data-points.
func TestRouteMapDivSurvivesUpdates(t *testing.T) {
	render := func(pts []Point) string {
		var buf bytes.Buffer
		if err := tmpl.ExecuteTemplate(&buf, "sec-route", Page{Route: route(pts), Live: true}); err != nil {
			t.Fatal(err)
		}
		return buf.String()
	}
	pts := sealedRecord().Points
	a, b := render(pts[:1]), render(pts)
	if mapDiv.FindString(a) == "" || mapDiv.FindString(a) != mapDiv.FindString(b) {
		t.Errorf("map div changed:\n%s\n%s", mapDiv.FindString(a), mapDiv.FindString(b))
	}
	if dataPoints.FindString(a) == dataPoints.FindString(b) {
		t.Error("data-points did not follow the new points")
	}
	if !strings.Contains(a, `data-key="route-box" data-live-children`) {
		t.Error("the route box is not updated in place")
	}
}

// The CSP lets the page load the map's tiles as images (and Leaflet's data:
// GIF, which is no request), and nothing else from anyone else.
func TestCSPAllowsOnlyTheMapTiles(t *testing.T) {
	for _, d := range strings.Split(contentSecurityPolicy, ";") {
		d = strings.TrimSpace(d)
		if strings.HasPrefix(d, "img-src ") {
			if d != "img-src 'self' data: "+MapTileHosts {
				t.Errorf("img-src = %q", d)
			}
			continue
		}
		if strings.Contains(d, "http") || strings.Contains(d, "*") {
			t.Errorf("%q reaches outside this server", d)
		}
	}
}

// route-map.js asks only the tile server for anything, and links only to
// the map data's credits (navigations the reader chooses).
func TestRouteMapRequestsOnlyTiles(t *testing.T) {
	b, err := fs.ReadFile(staticFS, "static/route-map.js")
	if err != nil {
		t.Fatal(err)
	}
	allowed := map[string]bool{
		"https://{s}.basemaps.cartocdn.com/rastertiles/": true,
		"https://www.openstreetmap.org/copyright":        true,
		"https://carto.com/attributions":                 true,
	}
	for _, m := range regexp.MustCompile(`https?://[^"' ]*|["']//|XMLHttpRequest|fetch\(|@import|WebSocket|sendBeacon`).FindAllString(string(b), -1) {
		if !allowed[m] {
			t.Errorf("route-map.js: %q", m)
		}
	}
	if !strings.HasPrefix(MapTileHosts, "https://*.basemaps.cartocdn.com") {
		t.Errorf("MapTileHosts = %q does not cover the tiles route-map.js loads", MapTileHosts)
	}
}

// The vendored Leaflet is the 1.9.4 release byte for byte (the SRI hashes
// on leafletjs.com/download.html and in the npm package).
func TestLeafletIsThePinnedRelease(t *testing.T) {
	for name, want := range map[string]string{
		"static/leaflet.js":  "20nQCchB9co0qIjJZRGuk2/Z9VM+kNiyxNV1lvTlZBo=",
		"static/leaflet.css": "p4NxAoJBhIIN+hmNHrzRCf9tD/miZyoHS5obTRR9BMY=",
	} {
		b, err := fs.ReadFile(staticFS, name)
		if err != nil {
			t.Fatal(err)
		}
		sum := sha256.Sum256(b)
		if got := base64.StdEncoding.EncodeToString(sum[:]); got != want {
			t.Errorf("%s sha256 = %s, want Leaflet 1.9.4's %s", name, got, want)
		}
	}
	if b, err := fs.ReadFile(staticFS, "static/Leaflet-LICENSE.txt"); err != nil || !bytes.Contains(b, []byte("BSD 2-Clause")) {
		t.Errorf("Leaflet's license is not shipped with it: %v", err)
	}
}

// The tiles' key reaches the page's body (route-map.js reads it there), on
// the family page and the central's case; without one the page carries none
// and route-map.js leaves the drawing alone.
func TestMapKeyReachesThePage(t *testing.T) {
	page := func(key string) string {
		mux := http.NewServeMux()
		NewHandler(fakeSource{testToken: sealedRecord()}, Options{CanSeal: true, Channel: "WhatsApp", MapKey: key}).Register(mux)
		rec := httptest.NewRecorder()
		mux.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/i/"+testToken, nil))
		return rec.Body.String()
	}
	if b := page("k3y-<x>"); !strings.Contains(b, `data-map-key="k3y-&lt;x&gt;"`) {
		t.Error("the family page lacks data-map-key")
	}
	if b := page(""); strings.Contains(b, "data-map-key") {
		t.Error("a page without a key carries data-map-key")
	}
	d := buildCentralCase(openRecord(), CaseExtras{}, Options{MapKey: "k3y", Now: centralNow})
	var buf bytes.Buffer
	if err := tmpl.ExecuteTemplate(&buf, "central-case", d); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(buf.String(), `data-open data-map-key="k3y"`) {
		t.Error("the central's case lacks data-map-key")
	}
}
