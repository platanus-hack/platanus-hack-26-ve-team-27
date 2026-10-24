// Package panel is the live emergencies web panel of the offline demo (#61):
// GET /demo/emergencies lists the most recently updated rows of the
// emergencies table so the jury can watch an alert arrive over HTTP, SMS or
// the mesh. It shows live locations, so it exists only with DEMO_PANEL_TOKEN
// set and is protected with HTTP Basic auth (any user name, the token as the
// password).
package panel

import (
	"context"
	"crypto/subtle"
	"fmt"
	"html/template"
	"log"
	"net/http"
	"strings"
	"time"

	"github.com/platanus-hack/platanus-hack-26-ve-team-27/server/internal/emergency/ingest"
)

// Limit is how many emergencies the panel lists.
const Limit = 50

// Lister reads the most recently updated emergencies; ingest.DBStore is one.
type Lister interface {
	ListRecent(ctx context.Context, limit int) ([]ingest.Emergency, error)
}

// Handler serves the panel.
type Handler struct {
	lister Lister // nil without a database: the panel answers 503
	token  string
	now    func() time.Time
}

// New returns the panel handler. token must not be empty (the caller mounts
// the route only when DEMO_PANEL_TOKEN is set). lister is nil without a
// database.
func New(lister Lister, token string) *Handler {
	return &Handler{lister: lister, token: token, now: time.Now}
}

// Register mounts GET /demo/emergencies.
func (h *Handler) Register(mux *http.ServeMux) {
	mux.HandleFunc("GET /demo/emergencies", h.serve)
}

// caracas is a fixed UTC-4 zone (Venezuela has no DST), so the server needs no
// tz database.
var caracas = time.FixedZone("VET", -4*3600)

const zoneLabel = "hora de Caracas (UTC-4)"

func (h *Handler) serve(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	if !h.authorized(r) {
		w.Header().Set("WWW-Authenticate", `Basic realm="MAMA demo"`)
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}
	if h.lister == nil {
		http.Error(w, "database not configured", http.StatusServiceUnavailable)
		return
	}
	list, err := h.lister.ListRecent(r.Context(), Limit)
	if err != nil {
		log.Printf("panel: list emergencies: %v", err)
		http.Error(w, "could not read emergencies", http.StatusInternalServerError)
		return
	}
	now := h.now()
	data := page{Zone: zoneLabel, Now: clock(now), Rows: make([]row, len(list))}
	for i, e := range list {
		data.Rows[i] = toRow(e, now)
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	if err := pageTmpl.Execute(w, data); err != nil {
		log.Printf("panel: render: %v", err)
	}
}

// authorized checks the Basic password against the token in constant time.
func (h *Handler) authorized(r *http.Request) bool {
	_, pass, ok := r.BasicAuth()
	return ok && h.token != "" && subtle.ConstantTimeCompare([]byte(pass), []byte(h.token)) == 1
}

type page struct {
	Zone string
	Now  string
	Rows []row
}

type row struct {
	ID, Device     string
	Status         string
	StatusClass    string
	Channels       string
	Hops           string
	First, Updated string
	Ago            string
	HasLocation    bool
	Coords         string
	MapURL         string
	LocationTime   string
	LocationAge    string
}

func toRow(e ingest.Emergency, now time.Time) row {
	r := row{
		ID:          ingest.FormatID(e.ID),
		Device:      shortDevice(e.DeviceID),
		Status:      statusText(e.Status),
		StatusClass: string(e.Status),
		Hops:        "desconocido",
		First:       clock(e.FirstReceivedAt),
		Updated:     clock(e.UpdatedAt),
		Ago:         ago(now.Sub(e.UpdatedAt)),
	}
	names := make([]string, len(e.Channels))
	for i, c := range e.Channels {
		names[i] = strings.ToUpper(string(c))
	}
	r.Channels = strings.Join(names, ", ")
	if e.MinHops != ingest.HopsUnknown {
		r.Hops = fmt.Sprint(e.MinHops)
	}
	if l := e.Location; l != nil {
		r.HasLocation = true
		r.Coords = fmt.Sprintf("%.5f, %.5f", l.Lat(), l.Lon())
		r.MapURL = fmt.Sprintf("https://www.openstreetmap.org/?mlat=%.5f&mlon=%.5f#map=17/%.5f/%.5f", l.Lat(), l.Lon(), l.Lat(), l.Lon())
		r.LocationTime = clock(l.SentAt)
		r.LocationAge = fmt.Sprintf("%d s", l.AgeS)
	}
	return r
}

func statusText(s ingest.Status) string {
	switch s {
	case ingest.StatusOpen:
		return "Abierta"
	case ingest.StatusAcked:
		return "Confirmada"
	case ingest.StatusCancelled:
		return "Cancelada"
	}
	return string(s)
}

// shortDevice is the first 8 characters of the device UUID: enough to tell
// phones apart on screen, no personal data.
func shortDevice(id string) string {
	if len(id) > 8 {
		return id[:8]
	}
	return id
}

func clock(t time.Time) string { return t.In(caracas).Format("02/01/2006 15:04:05") }

func ago(d time.Duration) string {
	switch s := int(d.Seconds()); {
	case s < 0:
		return "ahora"
	case s < 60:
		return fmt.Sprintf("hace %d s", s)
	case s < 3600:
		return fmt.Sprintf("hace %d min", s/60)
	case s < 86400:
		return fmt.Sprintf("hace %d h", s/3600)
	default:
		return fmt.Sprintf("hace %d d", s/86400)
	}
}

var pageTmpl = template.Must(template.New("panel").Parse(`<!doctype html>
<html lang="es">
<head>
<meta charset="utf-8">
<meta name="viewport" content="width=device-width, initial-scale=1">
<meta http-equiv="refresh" content="3">
<title>MAMÁ - Emergencias en vivo</title>
<style>
body{font-family:system-ui,sans-serif;margin:1.5rem;background:#fafafa;color:#222}
h1{margin:0 0 .25rem}
.meta{color:#555;margin-bottom:1rem}
table{border-collapse:collapse;width:100%;background:#fff}
th,td{border:1px solid #ddd;padding:.4rem .6rem;text-align:left;vertical-align:top}
th{background:#f0f0f0}
.open{color:#b00020;font-weight:bold}
.acked{color:#1b5e20;font-weight:bold}
.cancelled{color:#666}
code{font-size:.9em}
</style>
</head>
<body>
<h1>Emergencias en vivo</h1>
<p class="meta">Actualizado a las {{.Now}} ({{.Zone}}). La página se refresca cada 3 s.</p>
{{if .Rows}}
<table>
<thead><tr><th>Emergencia</th><th>Estado</th><th>Canales</th><th>Saltos</th><th>Primera recepción</th><th>Última actualización</th><th>Última ubicación</th></tr></thead>
<tbody>
{{range .Rows}}<tr>
<td><code>{{.ID}}</code><br>dispositivo <code>{{.Device}}</code></td>
<td class="{{.StatusClass}}">{{.Status}}</td>
<td>{{.Channels}}</td>
<td>{{.Hops}}</td>
<td>{{.First}}</td>
<td>{{.Updated}}<br>{{.Ago}}</td>
<td>{{if .HasLocation}}<a href="{{.MapURL}}" target="_blank" rel="noopener noreferrer">{{.Coords}}</a><br>enviada {{.LocationTime}}, antigüedad del fix {{.LocationAge}}{{else}}Sin ubicación{{end}}</td>
</tr>
{{end}}</tbody>
</table>
{{else}}
<p>Aún no hay emergencias. Cuando un teléfono envíe una alerta aparecerá aquí.</p>
{{end}}
</body>
</html>
`))
