package call

import (
	"encoding/json"
	"html/template"
	"log"
	"net/http"
	"regexp"
	"sort"
	"strings"
	"sync"
	"time"
)

// FallbackPath is the WebRTC fallback page (spikes/call/webrtc.html, #11).
const FallbackPath = "/call/fallback"

// fallbackWindow is how long an incident stays on the fallback page after
// its second +58 call failed.
const fallbackWindow = 15 * time.Minute

// agentIDRE is what the page accepts as an agent id, as the spike page did.
var agentIDRE = regexp.MustCompile(`^agent_[A-Za-z0-9]+$`)

// Fallback is the visible fallback for an incident whose +58 call failed
// twice (or could not be placed at all, such as a non-+58 number): Open logs the page URL, and the page at FallbackPath lists the open
// incidents and loads the ElevenLabs widget so "Mamá" talks through the
// browser. Without an open incident the page shows no agent id and no
// widget, so the public agent is only exposed while it is needed. The open
// incidents live in memory: a restart clears them. Create it with
// NewFallback.
type Fallback struct {
	agentID string
	url     string // the page URL for the log line
	now     func() time.Time

	mu   sync.Mutex
	open map[string]fallbackIncident // incident id -> when it opened and its call's variables
}

// NewFallback returns the fallback for the agent. publicURL is the server's
// public base URL (blank logs the bare path).
func NewFallback(agentID, publicURL string) *Fallback {
	return &Fallback{
		agentID: agentID,
		url:     strings.TrimSuffix(publicURL, "/") + FallbackPath,
		now:     time.Now,
		open:    map[string]fallbackIncident{},
	}
}

// Open puts the incident on the fallback page and logs where to find it.
// vars are the dynamic variables of the incident's phone call
// (CallVariables), which the browser call sends too. attempts is how many
// +58 calls were tried.
func (f *Fallback) Open(incidentID string, vars map[string]string, attempts int) {
	f.mu.Lock()
	f.open[incidentID] = fallbackIncident{ID: incidentID, At: f.now(), vars: vars}
	f.mu.Unlock()
	log.Printf("call fallback: incident %s: no +58 call connected after %d attempt(s); open %s for the WebRTC call", incidentID, attempts, f.url)
}

// pending returns the incidents opened within fallbackWindow, newest first,
// and forgets older ones.
func (f *Fallback) pending() []fallbackIncident {
	f.mu.Lock()
	defer f.mu.Unlock()
	now := f.now()
	var out []fallbackIncident
	for id, inc := range f.open {
		if now.Sub(inc.At) > fallbackWindow {
			delete(f.open, id)
			continue
		}
		out = append(out, inc)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].At.After(out[j].At) })
	return out
}

type fallbackIncident struct {
	ID   string
	At   time.Time
	vars map[string]string // the dynamic variables of its call
}

type fallbackPage struct {
	Incidents []fallbackIncident
	AgentID   string // blank: no widget
	// DynamicVars is the widget's dynamic-variables JSON, the same the phone
	// call sends: the incident's own when exactly one is open, so the browser
	// call's post-call webhook lands on that incident like the phone call's
	// would; with several open, the defaults and no incident_id, since the
	// page cannot tell which one the operator is calling about.
	DynamicVars string
	NoAgent     bool // incidents are open but ELEVENLABS_AGENT_ID is unusable
}

// ServeHTTP renders the page. It reloads itself every 5 s while nothing is
// pending, so the operator can keep it open on stage; once an incident is
// open it stops reloading, which would cut the browser call.
func (f *Fallback) ServeHTTP(w http.ResponseWriter, _ *http.Request) {
	p := fallbackPage{Incidents: f.pending()}
	if len(p.Incidents) > 0 {
		if agentIDRE.MatchString(f.agentID) {
			p.AgentID = f.agentID
			vars := CallVariables("", Callee{})
			if len(p.Incidents) == 1 {
				vars = CallVariables(p.Incidents[0].ID, Callee{})
				if p.Incidents[0].vars != nil {
					vars = p.Incidents[0].vars
				}
			}
			raw, _ := json.Marshal(vars)
			p.DynamicVars = string(raw)
		} else {
			p.NoAgent = true
		}
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	if err := fallbackTmpl.Execute(w, p); err != nil {
		log.Printf("call fallback: render: %v", err)
	}
}

var fallbackTmpl = template.Must(template.New("fallback").Parse(`<!doctype html>
<html lang="es">
<head>
  <meta charset="utf-8">
  <meta name="viewport" content="width=device-width, initial-scale=1">
  {{- if not .Incidents}}
  <meta http-equiv="refresh" content="5">
  {{- end}}
  <title>MAMÁ: llamada de respaldo</title>
  <style>
    :root { color-scheme: light dark; }
    body { margin: 0; padding: 16px; font: 16px/1.5 system-ui, sans-serif; background: Canvas; color: CanvasText; }
    main { max-width: 32rem; margin: 0 auto; }
    code { font-size: 0.9em; word-break: break-all; }
    .alert { padding: 12px; border: 2px solid #c00; border-radius: 8px; }
    .warn { padding: 12px; border: 1px solid #c60; border-radius: 8px; }
  </style>
</head>
<body>
<main>
  <h1>Llamada de respaldo (WebRTC)</h1>
  {{- if .Incidents}}
  <div class="alert">
    <p><strong>La llamada a +58 no entró.</strong> "Mamá" habla por el navegador con el mismo agente.
       Toca el botón y permite el micrófono.</p>
    <ul>
      {{- range .Incidents}}
      <li>Incidente <code>{{.ID}}</code> desde {{.At.UTC.Format "15:04:05"}} UTC</li>
      {{- end}}
    </ul>
  </div>
  {{- if .NoAgent}}
  <p class="warn">Falta ELEVENLABS_AGENT_ID en el servidor: no se puede cargar el agente.</p>
  {{- end}}
  {{- else}}
  <p>No hay llamadas de respaldo pendientes. Esta página se recarga sola cada 5 s.</p>
  {{- end}}
</main>
{{- if .AgentID}}
<elevenlabs-convai agent-id="{{.AgentID}}"{{if .DynamicVars}} dynamic-variables="{{.DynamicVars}}"{{end}}></elevenlabs-convai>
<script src="https://unpkg.com/@elevenlabs/convai-widget-embed@0.19.0" async type="text/javascript"></script>
{{- end}}
</body>
</html>
`))
