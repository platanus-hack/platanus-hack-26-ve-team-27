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
	"time"
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

// PDFPath is the detailed report as a PDF.
func PDFPath(token string) string { return PagePrefix + token + "/reporte.pdf" }

// SignaturePath is the seal's signature.
func SignaturePath(token string) string { return PagePrefix + token + "/ficha.sig" }

//go:embed templates/*.html
var templateFS embed.FS

//go:embed static
var staticFS embed.FS

var tmpl = template.Must(template.ParseFS(templateFS, "templates/*.html"))

// staticTypes are the only files StaticPrefix serves, with their type.
var staticTypes = map[string]string{
	".css":  "text/css; charset=utf-8",
	".js":   "text/javascript; charset=utf-8",
	".ttf":  "font/ttf",
	".webp": "image/webp",
	".txt":  "text/plain; charset=utf-8",
}

// contentSecurityPolicy allows only this server: opening a report makes no
// request to anyone else (no fonts, maps or scripts from third parties).
// connect-src 'self' is the live stream and the recording the waveform is
// drawn from; blob: is that same recording, handed to the player. Links the
// reader taps (Abrir en Maps) are navigations, not requests.
const contentSecurityPolicy = "default-src 'none'; style-src 'self'; font-src 'self'; img-src 'self'; " +
	"script-src 'self'; media-src 'self' blob:; connect-src 'self'; base-uri 'none'; form-action 'none'; frame-ancestors 'none'"

// Source is what Handler reads. DBStore is the real one.
type Source interface {
	// ByToken loads the record behind a live token; ErrNotFound for an
	// unknown, malformed or expired one.
	ByToken(ctx context.Context, token string) (Record, error)
	// Audio returns the recording of the n-th call (1-based; 0 = the newest
	// recorded) of the incident behind a live token; ErrNotFound when none.
	Audio(ctx context.Context, token string, n int) ([]byte, Recording, error)
}

// Handler serves the report routes. Create it with NewHandler.
type Handler struct {
	src     Source
	opts    Options
	closer  *Closer
	demo    *Demo
	kapso   *KapsoWebhook
	central *Central

	// Live stream timing; the defaults when zero (tests shorten them).
	LivePoll  time.Duration
	KeepAlive time.Duration
	MaxStream time.Duration
	Now       func() time.Time // time.Now when nil
}

// NewHandler serves reports from src. opts.Token is ignored (each request
// has its own).
func NewHandler(src Source, opts Options) *Handler {
	return &Handler{src: src, opts: opts}
}

// WithKapso mounts POST /webhooks/kapso (KapsoWebhook).
func (h *Handler) WithKapso(k *KapsoWebhook) *Handler {
	h.kapso = k
	return h
}

// WithDemo mounts the presenter's demo routes (Demo).
func (h *Handler) WithDemo(d *Demo) *Handler {
	h.demo = d
	return h
}

func (h *Handler) now() time.Time {
	if h.Now != nil {
		return h.Now()
	}
	return time.Now()
}

// RecoverWith lets a page view restart an unfinished close (Closer.Recover).
func (h *Handler) RecoverWith(c *Closer) *Handler {
	h.closer = c
	return h
}

// Register mounts GET /i/{token}, its recording, the sealed document and
// signature, the static files, and the extras mounted with WithDemo,
// WithKapso and WithCentral.
func (h *Handler) Register(mux *http.ServeMux) {
	mux.HandleFunc("GET "+PagePrefix+"{token}", h.page)
	mux.HandleFunc("GET "+PagePrefix+"{token}/audio.mp3", h.audio)
	mux.HandleFunc("GET "+PagePrefix+"{token}/ficha.json", h.document)
	mux.HandleFunc("GET "+PagePrefix+"{token}/ficha.sig", h.signature)
	mux.HandleFunc("GET "+PagePrefix+"{token}/live", h.live)
	mux.HandleFunc("GET "+PagePrefix+"{token}/estado.json", h.state)
	mux.HandleFunc("GET "+PagePrefix+"{token}/reporte.pdf", h.pdf)
	if h.demo != nil {
		h.demo.register(mux)
	}
	if h.kapso != nil {
		mux.Handle("POST /webhooks/kapso", h.kapso)
	}
	h.registerCentral(mux)
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
	h.closer.Recover(rec)
	opts := h.opts
	opts.Token = token
	opts.Now = h.now()
	render(w, http.StatusOK, "page", Build(rec, opts))
}

// document serves the sealed ficha.json byte for byte: the signature covers
// these exact bytes. 404 until the report is sealed.
func (h *Handler) document(w http.ResponseWriter, r *http.Request) {
	rec, _, ok := h.load(w, r)
	if !ok {
		return
	}
	if rec.Seal == nil {
		notFound(w)
		return
	}
	download(w, "application/json", "ficha.json", rec.Seal.Document)
}

// signature serves ficha.sig: the seal's signature in Base64.
func (h *Handler) signature(w http.ResponseWriter, r *http.Request) {
	rec, _, ok := h.load(w, r)
	if !ok {
		return
	}
	if rec.Seal == nil {
		notFound(w)
		return
	}
	download(w, "text/plain; charset=utf-8", "ficha.sig", EncodeSignature(rec.Seal.Signature))
}

func download(w http.ResponseWriter, typ, name string, b []byte) {
	w.Header().Set("Content-Type", typ)
	w.Header().Set("Content-Disposition", `attachment; filename="`+name+`"`)
	w.Header().Set("Content-Length", strconv.Itoa(len(b)))
	_, _ = w.Write(b)
}

// audio serves a call's recording from our own server (never a link to
// ElevenLabs), with Range support so the player can seek. ?llamada=N picks
// the call (1-based); without it, the newest recorded call.
func (h *Handler) audio(w http.ResponseWriter, r *http.Request) {
	privacy(w)
	n := 0
	if v := r.URL.Query().Get("llamada"); v != "" {
		var err error
		if n, err = strconv.Atoi(v); err != nil || n < 1 {
			notFound(w)
			return
		}
	}
	audio, rec, err := h.src.Audio(r.Context(), r.PathValue("token"), n)
	if errors.Is(err, ErrNotFound) {
		notFound(w)
		return
	}
	if err != nil {
		log.Printf("report: audio: %v", err)
		http.Error(w, "No se pudo cargar la grabación.", http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", rec.ContentType)
	w.Header().Set("Content-Disposition", `inline; filename="llamada.mp3"`)
	http.ServeContent(w, r, "", rec.StoredAt, bytes.NewReader(audio))
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
