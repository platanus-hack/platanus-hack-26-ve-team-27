package report

import (
	"context"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/json"
	"errors"
	"log"
	"net/http"
	"net/url"
	"time"
)

// Demo serves the presenter's shortcuts to the newest incident's report,
// only with DEMO_KEY (mounted by NewHandler.WithDemo):
//
//   - GET /demo/latest?key=K redirects to the newest incident's report.
//   - GET /demo/next?key=K waits for the next incident (open it, then make the
//     gesture on the phone) and opens its report as soon as it arrives.
//
// A missing or wrong key answers 404, as if the routes did not exist.
type Demo struct {
	Key     string // DEMO_KEY; blank never matches
	Store   DemoStore
	TTLDays int
	Now     func() time.Time // time.Now when nil
}

// DemoStore is what Demo reads. DBStore is the real one.
type DemoStore interface {
	NewestIncident(ctx context.Context, after time.Time) (string, error)
	Ensure(ctx context.Context, incidentID string, ttlDays int) (Link, error)
}

func (d *Demo) register(mux *http.ServeMux) {
	mux.HandleFunc("GET /demo/latest", d.latest)
	mux.HandleFunc("GET /demo/next", d.next)
	mux.HandleFunc("GET /demo/next.json", d.nextJSON)
}

// authorized compares the key in constant time (over SHA-256 digests, so
// the length does not leak either).
func (d *Demo) authorized(r *http.Request) bool {
	if d.Key == "" {
		return false
	}
	got := sha256.Sum256([]byte(r.URL.Query().Get("key")))
	want := sha256.Sum256([]byte(d.Key))
	return subtle.ConstantTimeCompare(got[:], want[:]) == 1
}

// link is the report path of the newest incident created after after.
func (d *Demo) link(ctx context.Context, after time.Time) (string, error) {
	id, err := d.Store.NewestIncident(ctx, after)
	if err != nil {
		return "", err
	}
	l, err := d.Store.Ensure(ctx, id, d.TTLDays)
	if err != nil {
		return "", err
	}
	return PageURL("", l.Token), nil
}

func (d *Demo) latest(w http.ResponseWriter, r *http.Request) {
	privacy(w)
	if !d.authorized(r) {
		notFound(w)
		return
	}
	u, err := d.link(r.Context(), time.Time{})
	if errors.Is(err, ErrUnknownIncident) {
		notFound(w)
		return
	}
	if err != nil {
		log.Printf("report: demo: %v", err)
		http.Error(w, "No se pudo abrir la ficha.", http.StatusInternalServerError)
		return
	}
	http.Redirect(w, r, u, http.StatusSeeOther)
}

// next renders the waiting page; its script polls next.json with the time
// the page opened, and follows the URL once an incident newer than that
// exists.
func (d *Demo) next(w http.ResponseWriter, r *http.Request) {
	privacy(w)
	if !d.authorized(r) {
		notFound(w)
		return
	}
	q := url.Values{"key": {r.URL.Query().Get("key")}, "since": {iso(d.now())}}
	render(w, http.StatusOK, "demo-wait", "/demo/next.json?"+q.Encode())
}

func (d *Demo) nextJSON(w http.ResponseWriter, r *http.Request) {
	privacy(w)
	if !d.authorized(r) {
		notFound(w)
		return
	}
	since, err := time.Parse(time.RFC3339Nano, r.URL.Query().Get("since"))
	if err != nil {
		http.Error(w, "since must be RFC 3339", http.StatusBadRequest)
		return
	}
	u, err := d.link(r.Context(), since)
	out := map[string]any{"waiting": true}
	switch {
	case errors.Is(err, ErrUnknownIncident):
	case err != nil:
		log.Printf("report: demo: %v", err)
		http.Error(w, "No se pudo buscar la alerta.", http.StatusInternalServerError)
		return
	default:
		out = map[string]any{"url": u}
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(out)
}

func (d *Demo) now() time.Time {
	if d.Now != nil {
		return d.Now()
	}
	return time.Now()
}
