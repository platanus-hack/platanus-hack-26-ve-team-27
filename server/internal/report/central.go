package report

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/hex"
	"encoding/json"
	"errors"
	"log"
	"net/http"
	"net/url"
	"time"
)

// The central dashboard (#99): GET /central/ lists every incident as it
// arrives, newest first, and GET /central/r/{incident_id} follows one case
// live, on the same sections and live loop as the family page. It sees every
// incident, so it is never public: CENTRAL_KEY, given once as ?key=, becomes
// a cookie; without it every /central route answers 404.
const (
	CentralPrefix = "/central/"
	// centralCookie holds HMAC(CENTRAL_KEY, centralCookieContext), never
	// the key: changing the key logs every screen out.
	centralCookie        = "mama_central"
	centralCookieContext = "MAMA-central-cookie-v1"
	centralCookieMaxAge  = 24 * time.Hour
	defaultCentralRows   = 40
)

// CentralCasePath is a case's page on the central dashboard.
func CentralCasePath(incidentID string) string { return CentralPrefix + "r/" + incidentID }

// CentralSections are the central list's live sections.
var CentralSections = []string{"c-counts", "c-list"}

// CentralCaseSections are the central detail's live sections: the family
// page's status, track, call and route (with the points' times), and the
// central's own header (with the call's one-line summary) and notified
// people. The horizontal track is its only timeline.
var CentralCaseSections = []string{"status", "c-head", "track", "call", "route", "c-family"}

// Central is the central dashboard, mounted by Handler.WithCentral.
type Central struct {
	Key   string // CENTRAL_KEY; blank leaves the dashboard unmounted
	Store CentralStore
	// TTLDays is the life of the report link the detail creates when the
	// case has none yet: its recording and PDF are served from /i/{token}.
	TTLDays int
	Rows    int // how many cases the list shows; defaultCentralRows when 0
}

// CentralStore is what the dashboard reads. DBStore is the real one.
type CentralStore interface {
	Cases(ctx context.Context, limit int) ([]CaseItem, error)
	Load(ctx context.Context, incidentID string) (Record, error)
	Extras(ctx context.Context, incidentID string) (CaseExtras, error)
	Ensure(ctx context.Context, incidentID string, ttlDays int) (Link, error)
}

// WithCentral mounts the central dashboard; nil or a blank Key leaves it
// off (every /central route answers 404).
func (h *Handler) WithCentral(c *Central) *Handler {
	h.central = c
	return h
}

func (h *Handler) registerCentral(mux *http.ServeMux) {
	c := h.central
	if c == nil || c.Key == "" || c.Store == nil {
		return
	}
	mux.HandleFunc("GET /central", c.gate(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, CentralPrefix, http.StatusSeeOther)
	}))
	mux.HandleFunc("GET "+CentralPrefix+"{$}", c.gate(h.centralList))
	mux.HandleFunc("GET "+CentralPrefix+"live", c.gate(h.centralListLive))
	mux.HandleFunc("GET "+CentralPrefix+"estado.json", c.gate(h.centralListState))
	mux.HandleFunc("GET "+CentralPrefix+"r/{id}", c.gate(h.centralCase))
	mux.HandleFunc("GET "+CentralPrefix+"r/{id}/live", c.gate(h.centralCaseLive))
	mux.HandleFunc("GET "+CentralPrefix+"r/{id}/estado.json", c.gate(h.centralCaseState))
}

// ---------------------------------------------------------------- access

// gate lets a request through only with the key: ?key= right sets the
// cookie and redirects to the same URL without it (so the key leaves the
// address bar and the history); a valid cookie passes. Anything else, a
// wrong key included, is 404, as if the dashboard did not exist. Every
// answer carries the report's privacy headers.
func (c *Central) gate(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		privacy(w)
		q := r.URL.Query()
		if q.Has("key") {
			if !c.keyMatches(q.Get("key")) {
				centralNotFound(w)
				return
			}
			http.SetCookie(w, &http.Cookie{
				Name: centralCookie, Value: c.cookieValue(), Path: "/central",
				MaxAge: int(centralCookieMaxAge.Seconds()), HttpOnly: true, Secure: true, SameSite: http.SameSiteStrictMode,
			})
			q.Del("key")
			u := url.URL{Path: r.URL.Path, RawQuery: q.Encode()}
			http.Redirect(w, r, u.String(), http.StatusSeeOther)
			return
		}
		if !c.cookieMatches(r) {
			centralNotFound(w)
			return
		}
		next(w, r)
	}
}

// keyMatches compares in constant time, over SHA-256 digests so the length
// does not leak either.
func (c *Central) keyMatches(got string) bool {
	if c.Key == "" {
		return false
	}
	g, want := sha256.Sum256([]byte(got)), sha256.Sum256([]byte(c.Key))
	return subtle.ConstantTimeCompare(g[:], want[:]) == 1
}

func (c *Central) cookieValue() string {
	m := hmac.New(sha256.New, []byte(c.Key))
	m.Write([]byte(centralCookieContext))
	return hex.EncodeToString(m.Sum(nil))
}

func (c *Central) cookieMatches(r *http.Request) bool {
	ck, err := r.Cookie(centralCookie)
	if err != nil || c.Key == "" {
		return false
	}
	return subtle.ConstantTimeCompare([]byte(ck.Value), []byte(c.cookieValue())) == 1
}

// centralNotFound is the 404 every refused or unknown central request gets:
// the same body as a route that does not exist.
func centralNotFound(w http.ResponseWriter) {
	privacy(w)
	http.Error(w, "404 page not found", http.StatusNotFound)
}

// theme is the page's starting theme: dark, the projector's, unless
// ?tema=claro. central.js then keeps the viewer's choice.
func theme(r *http.Request) string {
	if r.URL.Query().Get("tema") == "claro" {
		return "light"
	}
	return "dark"
}

// ---------------------------------------------------------------- the list

// centralList is what the list page renders.
type centralList struct {
	Theme  string
	Rows   []CaseRow
	Counts []CaseCount
	Total  int
	Follow bool // ?seguir=1: open the next new case by itself
}

func (h *Handler) centralCases(ctx context.Context) (centralList, error) {
	n := h.central.Rows
	if n <= 0 {
		n = defaultCentralRows
	}
	items, err := h.central.Store.Cases(ctx, n)
	if err != nil {
		return centralList{}, err
	}
	now := h.now()
	l := centralList{Rows: make([]CaseRow, 0, len(items)), Total: len(items)}
	for _, it := range items {
		l.Rows = append(l.Rows, caseRow(it, now))
	}
	l.Counts = caseCounts(l.Rows)
	return l, nil
}

// centralListSnapshot renders the list's live sections. It is always live
// and never over, whatever each case's state: the list waits for the next
// case.
func (h *Handler) centralListSnapshot(ctx context.Context) (Snapshot, error) {
	l, err := h.centralCases(ctx)
	if err != nil {
		return Snapshot{}, err
	}
	return renderSnapshot(l, "list", true, false, h.now(), CentralSections)
}

func (h *Handler) centralList(w http.ResponseWriter, r *http.Request) {
	l, err := h.centralCases(r.Context())
	if err != nil {
		log.Printf("report: central: list: %v", err)
		http.Error(w, "No se pudieron cargar los reportes. Intenta de nuevo en un momento.", http.StatusInternalServerError)
		return
	}
	l.Theme = theme(r)
	l.Follow = r.URL.Query().Get("seguir") == "1"
	render(w, http.StatusOK, "central-list", l)
}

// centralListLive is the list's stream: the same loop as the family page,
// with the list as its snapshot.
func (h *Handler) centralListLive(w http.ResponseWriter, r *http.Request) {
	if _, err := h.centralListSnapshot(r.Context()); err != nil {
		log.Printf("report: central: list: %v", err)
		http.Error(w, "No se pudieron cargar los reportes.", http.StatusInternalServerError)
		return
	}
	h.serveSnapshots(w, r, h.centralListSnapshot)
}

func (h *Handler) centralListState(w http.ResponseWriter, r *http.Request) {
	s, err := h.centralListSnapshot(r.Context())
	if err != nil {
		log.Printf("report: central: list: %v", err)
		http.Error(w, "No se pudieron cargar los reportes.", http.StatusInternalServerError)
		return
	}
	writeJSON(w, s)
}

// ---------------------------------------------------------------- one case

// centralCase is what the detail page renders: the family page's Page plus
// the central's header and notified people.
type centralCase struct {
	Page
	Theme string
	Case  CaseRow
	// Situation is the call's one-line summary from #106's ficha
	// (calls.ficha.situacion), shown under the name; "" without one.
	Situation    string
	Notified     NotifiedView
	NotifiedNote string // the notified panel's line when nobody was
	at           time.Time
	follow       bool // the detail's stream stays open (see centralCaseSnapshot)
}

// The central's route drawing: a little taller than the family page's
// (16:10, so it fills its column at 1440x900 and at 1280x720) and with room
// at the edges for the time labels.
const (
	centralRouteW, centralRouteH, centralRoutePad = 320.0, 200.0, 30.0
)

// buildCentralCase turns the record into the detail page. token is the
// case's report link, for its recording and PDF.
func buildCentralCase(rec Record, x CaseExtras, opts Options) centralCase {
	page := Build(rec, opts)
	// The family page shows the transcript once the call ends; the central
	// shows it while it happens, as soon as turns are stored (#97 appends
	// them to calls.transcript during the call).
	if page.Call == nil && page.CallActive {
		for i := len(page.Calls) - 1; i >= 0; i-- {
			if len(page.Calls[i].Turns) > 0 {
				page.Call = &page.Calls[i]
				break
			}
		}
	}
	page.Route = centralRoute(rec.Points)
	item := caseItemOf(rec, x)
	d := centralCase{Page: page, Case: caseRow(item, opts.Now), Situation: situationOf(x.Ficha), at: opts.Now}
	d.follow = page.Live || (rec.Seal != nil && opts.Now.Sub(rec.Seal.SealedAt) < followSeal)
	if d.Case.State == CaseCancelled {
		d.Title, d.TitleKind = "Cancelado por la persona", "cancelled"
	}
	d.Notified = notifiedView(page.Family, MaskRecipient(rec.Device.PhoneNumber))
	if len(page.Family) == 0 {
		d.NotifiedNote = page.FamilyNote
		if d.Case.State == CaseCancelled && rec.Incident.NotifiedAt == nil {
			d.NotifiedNote = "La persona canceló: no se avisó a nadie."
		}
	}
	return d
}

// centralCaseOf loads a case and renders it at now.
func (h *Handler) centralCaseOf(ctx context.Context, id, token string) (centralCase, error) {
	rec, err := h.central.Store.Load(ctx, id)
	if err != nil {
		return centralCase{}, err
	}
	x, err := h.central.Store.Extras(ctx, id)
	if err != nil {
		return centralCase{}, err
	}
	h.closer.Recover(rec)
	opts := h.opts
	opts.Token, opts.Now = token, h.now()
	return buildCentralCase(rec, x, opts), nil
}

// centralCaseSnapshot renders the detail's live sections. The state is the
// central's (alerta, llamada…), for the page's colors. It stays live while
// the case is (d.Live) and for followSeal after its seal, since delivery
// and read receipts keep coming after it; then the loop ends the stream.
func centralCaseSnapshot(d centralCase, now time.Time) (Snapshot, error) {
	return renderSnapshot(d, d.Case.State, d.follow, false, now, CentralCaseSections)
}

// followSeal is how long the central detail keeps following a sealed case:
// Kapso's delivered and read receipts arrive after the seal.
const followSeal = 15 * time.Minute

// caseToken is the case's report link token, creating the link when the
// case has none yet (the family message has not gone out): the detail plays
// the recording and offers the PDF from /i/{token}.
func (h *Handler) caseToken(ctx context.Context, id string) (string, error) {
	ttl := h.central.TTLDays
	if ttl <= 0 {
		ttl = 30
	}
	l, err := h.central.Store.Ensure(ctx, id, ttl)
	return l.Token, err
}

// loadCase answers 404 for an unknown or malformed id and 500 on a store
// failure, and returns the case's token otherwise.
func (h *Handler) loadCase(w http.ResponseWriter, r *http.Request) (string, string, bool) {
	id := r.PathValue("id")
	token, err := h.caseToken(r.Context(), id)
	if errors.Is(err, ErrUnknownIncident) || errors.Is(err, ErrNotFound) {
		centralNotFound(w)
		return "", "", false
	}
	if err != nil {
		log.Printf("report: central: case: %v", err)
		http.Error(w, "No se pudo cargar el caso. Intenta de nuevo en un momento.", http.StatusInternalServerError)
		return "", "", false
	}
	return id, token, true
}

func (h *Handler) centralCase(w http.ResponseWriter, r *http.Request) {
	id, token, ok := h.loadCase(w, r)
	if !ok {
		return
	}
	d, err := h.centralCaseOf(r.Context(), id, token)
	if errors.Is(err, ErrUnknownIncident) {
		centralNotFound(w)
		return
	}
	if err != nil {
		log.Printf("report: central: case: %v", err)
		http.Error(w, "No se pudo cargar el caso. Intenta de nuevo en un momento.", http.StatusInternalServerError)
		return
	}
	d.Theme = theme(r)
	render(w, http.StatusOK, "central-case", d)
}

func (h *Handler) centralCaseLive(w http.ResponseWriter, r *http.Request) {
	id, token, ok := h.loadCase(w, r)
	if !ok {
		return
	}
	h.serveSnapshots(w, r, func(ctx context.Context) (Snapshot, error) {
		d, err := h.centralCaseOf(ctx, id, token)
		if err != nil {
			return Snapshot{}, err
		}
		return centralCaseSnapshot(d, d.at)
	})
}

func (h *Handler) centralCaseState(w http.ResponseWriter, r *http.Request) {
	id, token, ok := h.loadCase(w, r)
	if !ok {
		return
	}
	d, err := h.centralCaseOf(r.Context(), id, token)
	if err == nil {
		var s Snapshot
		if s, err = centralCaseSnapshot(d, d.at); err == nil {
			writeJSON(w, s)
			return
		}
	}
	if errors.Is(err, ErrUnknownIncident) {
		centralNotFound(w)
		return
	}
	log.Printf("report: central: estado: %v", err)
	http.Error(w, "No se pudo cargar el caso.", http.StatusInternalServerError)
}

func writeJSON(w http.ResponseWriter, v any) {
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(v)
}
