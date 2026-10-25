package report

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"io/fs"
	"net/http"
	"path"
	"regexp"
	"strings"
	"time"
)

// The static files (CSS, JS, fonts, images) are served at fixed paths under
// StaticPrefix, so a browser that cached an older build would pair new HTML
// with old CSS and JS, and show a broken page. Every URL the pages emit
// therefore carries the file's content version (?v=<hash>), computed once
// at startup from the embedded files: a new build changes the URL, and the
// versioned URL can be cached for good. A request without the current
// version (an old cached page, a hand-typed URL) is answered no-cache with
// an ETag, so it is revalidated.

// asset is one embedded static file as served.
type asset struct {
	body    []byte
	version string // contentVersion(body)
}

// assets are the static files by name. CSS is stored with its url()
// references to other static files already versioned, and hashed after
// that, so a new font also gives the CSS that loads it a new version.
var assets = loadAssets(staticFS, "static")

// contentVersion is the first 10 hex digits of the SHA-256 of b.
func contentVersion(b []byte) string {
	sum := sha256.Sum256(b)
	return hex.EncodeToString(sum[:])[:10]
}

// loadAssets reads every file of dir in fsys with a type in staticTypes.
func loadAssets(fsys fs.FS, dir string) map[string]asset {
	entries, err := fs.ReadDir(fsys, dir)
	if err != nil {
		panic("report: static files: " + err.Error())
	}
	out := map[string]asset{}
	var css []string
	for _, e := range entries {
		name := e.Name()
		if _, ok := staticTypes[path.Ext(name)]; !ok || e.IsDir() {
			continue
		}
		if path.Ext(name) == ".css" {
			css = append(css, name) // after the files they point at
			continue
		}
		b, err := fs.ReadFile(fsys, dir+"/"+name)
		if err != nil {
			panic("report: static files: " + err.Error())
		}
		out[name] = asset{body: b, version: contentVersion(b)}
	}
	for _, name := range css {
		b, err := fs.ReadFile(fsys, dir+"/"+name)
		if err != nil {
			panic("report: static files: " + err.Error())
		}
		b = versionCSSURLs(b, out)
		out[name] = asset{body: b, version: contentVersion(b)}
	}
	return out
}

// cssStaticURL matches url("/ficha/static/<file>") in CSS, quoted or not.
var cssStaticURL = regexp.MustCompile(`url\((["']?)` + regexp.QuoteMeta(StaticPrefix) + `([A-Za-z0-9._-]+)(["']?)\)`)

// versionCSSURLs adds ?v= to each url() that points at a known static file.
func versionCSSURLs(css []byte, known map[string]asset) []byte {
	return cssStaticURL.ReplaceAllFunc(css, func(m []byte) []byte {
		sub := cssStaticURL.FindSubmatch(m)
		a, ok := known[string(sub[2])]
		if !ok {
			return m
		}
		return []byte("url(" + string(sub[1]) + StaticPrefix + string(sub[2]) + "?v=" + a.version + string(sub[3]) + ")")
	})
}

// StaticURL is the versioned URL of an embedded static file, for the
// templates ({{static "ficha.css"}}). A name that is not embedded gets the
// bare path, which answers 404.
func StaticURL(name string) string {
	if a, ok := assets[name]; ok {
		return StaticPrefix + name + "?v=" + a.version
	}
	return StaticPrefix + name
}

// Cache lifetimes of the static files.
const (
	cacheVersioned   = "public, max-age=31536000, immutable" // the URL changes with the bytes
	cacheUnversioned = "no-cache"                            // revalidate with the ETag
)

// serveStatic serves an embedded file. They hold nothing private. With the
// current ?v= it may be cached for a year; without it (or with an old one)
// it must be revalidated. The Referer is dropped either way.
func serveStatic(w http.ResponseWriter, r *http.Request) {
	name := r.PathValue("file")
	typ, ok := staticTypes[path.Ext(name)]
	a, found := assets[name]
	if !ok || !found || strings.Contains(name, "/") {
		http.NotFound(w, r)
		return
	}
	hdr := w.Header()
	hdr.Set("Content-Type", typ)
	hdr.Set("ETag", `"`+a.version+`"`)
	if r.URL.Query().Get("v") == a.version {
		hdr.Set("Cache-Control", cacheVersioned)
	} else {
		hdr.Set("Cache-Control", cacheUnversioned)
	}
	hdr.Set("Referrer-Policy", "no-referrer")
	hdr.Set("X-Content-Type-Options", "nosniff")
	http.ServeContent(w, r, "", time.Time{}, bytes.NewReader(a.body)) // answers If-None-Match with 304
}
