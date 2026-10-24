package report

import (
	"bytes"
	"context"
	"embed"
	"errors"
	"html/template"
	"io/fs"
	"log"
	"net/http"
	"path"
	"strconv"
	"strings"
)

// Route prefixes. The page and its files live under /i/{token}; the shared
// static files (fonts, images, CSS, JS) under StaticPrefix, with no token.
const (
	PagePrefix   = "/i/"
	StaticPrefix = "/ficha/static/"
)

// PageURL is the report page's URL on base (blank gives the bare path).
func PageURL(base, token string) string {
	return strings.TrimSuffix(base, "/") + PagePrefix + token
}

// AudioPath is the recording of the incident's n-th call (1-based).
func AudioPath(token string, n int) string {
	return PagePrefix + token + "/audio.mp3?llamada=" + strconv.Itoa(n)
}

// DocumentPath is the sealed document, served byte for byte.
func DocumentPath(token string) string { return PagePrefix + token + "/ficha.json" }

// SignaturePath is the seal's signature.
func SignaturePath(token string) string { return PagePrefix + token + "/ficha.sig" }

//go:embed templates/ficha.html
var templateFS embed.FS

//go:embed static
var staticFS embed.FS

var tmpl = template.Must(template.ParseFS(templateFS, "templates/ficha.html"))

// staticTypes are the only files StaticPrefix serves, with their type.
var staticTypes = map[string]string{
	".css":  "text/css; charset=utf-8",
	".js":   "text/javascript; charset=utf-8",
	".ttf":  "font/ttf",
	".webp": "image/webp",
	".svg":  "image/svg+xml",
	".txt":  "text/plain; charset=utf-8",
}

// contentSecurityPolicy allows only this server: opening a report makes no
// request to anyone else (no fonts, maps or scripts from third parties).
// Links the reader taps (Abrir en Maps) are navigations, not requests.
const contentSecurityPolicy = "default-src 'none'; style-src 'self'; font-src 'self'; img-src 'self'; " +
	"script-src 'self'; media-src 'self'; connect-src 'none'; base-uri 'none'; form-action 'none'; frame-ancestors 'none'"

// Source is what Handler reads. DBStore is the real one.
type Source interface {
	// ByToken loads the record behind a live token; ErrNotFound for an
	// unknown, malformed or expired one.
	ByToken(ctx context.Context, token string) (Record, error)
}

// Handler serves the report routes. Create it with NewHandler.
type Handler struct {
	src  Source
	opts Options
}

// NewHandler serves reports from src. opts.Token is ignored (each request
// has its own).
func NewHandler(src Source, opts Options) *Handler {
	return &Handler{src: src, opts: opts}
}

// Register mounts GET /i/{token} and the static files.
func (h *Handler) Register(mux *http.ServeMux) {
	mux.HandleFunc("GET "+PagePrefix+"{token}", h.page)
	mux.HandleFunc("GET "+StaticPrefix+"{file}", serveStatic)
}

// privacy sets the headers every report response carries: no Referer to
// anyone (the token is in the URL), no indexing, no caching.
func privacy(w http.ResponseWriter) {
	hdr := w.Header()
	hdr.Set("Referrer-Policy", "no-referrer")
	hdr.Set("X-Robots-Tag", "noindex, nofollow, noarchive")
	hdr.Set("Cache-Control", "no-store")
	hdr.Set("X-Content-Type-Options", "nosniff")
	hdr.Set("Content-Security-Policy", contentSecurityPolicy)
}

// load reads the record behind the request's token, answering 404 (unknown,
// malformed or expired: all alike) or 500 itself when it cannot.
func (h *Handler) load(w http.ResponseWriter, r *http.Request) (Record, string, bool) {
	privacy(w)
	token := r.PathValue("token")
	rec, err := h.src.ByToken(r.Context(), token)
	if errors.Is(err, ErrNotFound) {
		notFound(w)
		return Record{}, "", false
	}
	if err != nil {
		log.Printf("report: load: %v", err) // never the token: it is the secret
		http.Error(w, "No se pudo cargar la ficha. Intenta de nuevo en un momento.", http.StatusInternalServerError)
		return Record{}, "", false
	}
	return rec, token, true
}

func (h *Handler) page(w http.ResponseWriter, r *http.Request) {
	rec, token, ok := h.load(w, r)
	if !ok {
		return
	}
	opts := h.opts
	opts.Token = token
	render(w, http.StatusOK, "page", Build(rec, opts))
}

func notFound(w http.ResponseWriter) {
	privacy(w)
	render(w, http.StatusNotFound, "missing", nil)
}

func render(w http.ResponseWriter, status int, name string, data any) {
	var buf bytes.Buffer
	if err := tmpl.ExecuteTemplate(&buf, name, data); err != nil {
		log.Printf("report: render %s: %v", name, err)
		http.Error(w, "No se pudo mostrar la ficha.", http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.WriteHeader(status)
	_, _ = w.Write(buf.Bytes())
}

// serveStatic serves an embedded file. They hold nothing private, so they
// may be cached; the Referer is still dropped.
func serveStatic(w http.ResponseWriter, r *http.Request) {
	name := r.PathValue("file")
	typ, ok := staticTypes[path.Ext(name)]
	if !ok || strings.Contains(name, "/") {
		http.NotFound(w, r)
		return
	}
	b, err := fs.ReadFile(staticFS, "static/"+name)
	if err != nil {
		http.NotFound(w, r)
		return
	}
	hdr := w.Header()
	hdr.Set("Content-Type", typ)
	hdr.Set("Cache-Control", "public, max-age=86400")
	hdr.Set("Referrer-Policy", "no-referrer")
	hdr.Set("X-Content-Type-Options", "nosniff")
	_, _ = w.Write(b)
}
