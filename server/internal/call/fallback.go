package call

import (
	"crypto/subtle"
	"encoding/json"
	"html/template"
	"log"
	"net/http"
	"regexp"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/platanus-hack/platanus-hack-26-ve-team-27/server/internal/contract"
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
//
// The browser call carries the duress and safe phrases, so with a token the
// page answers 401 unless the request has ?token=<token>. Without a token
// (FALLBACK_TOKEN unset) the page stays open, as it was, but sends the
// phrases empty: a URL anyone can guess never shows them.
type Fallback struct {
	agentID string
	url     string // the page URL for the log line, without the token
	token   string // FALLBACK_TOKEN; blank: open page, no phrases
	now     func() time.Time

	mu   sync.Mutex
	open map[string]fallbackIncident // incident id -> when it opened and its call's variables
}

// NewFallback returns the fallback for the agent. publicURL is the server's
// public base URL (blank logs the bare path). token is FALLBACK_TOKEN.
func NewFallback(agentID, publicURL, token string) *Fallback {
	url := strings.TrimSuffix(publicURL, "/") + FallbackPath
	if token != "" {
		url += "?token=<FALLBACK_TOKEN>" // never the value: logs are not secret
	}
	return &Fallback{
		agentID: agentID,
		url:     url,
		token:   token,
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

// withoutPhrases returns a copy of vars with the duress and safe phrases
// blanked; every variable is still there, so ElevenLabs takes the call.
func withoutPhrases(vars map[string]string) map[string]string {
	out := make(map[string]string, len(vars))
	for k, v := range vars {
		out[k] = v
	}
	out[contract.DynamicVarDuressPhrase] = ""
	out[contract.DynamicVarSafePhrase] = ""
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
// open it stops reloading, which would cut the browser call. With a token,
// a request without it gets 401 and nothing else.
func (f *Fallback) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	// The token rides in the URL: keep it out of the Referer the widget
	// script's CDN would get.
	w.Header().Set("Referrer-Policy", "no-referrer")
	w.Header().Set("Cache-Control", "no-store")
	if f.token != "" && subtle.ConstantTimeCompare([]byte(r.URL.Query().Get("token")), []byte(f.token)) != 1 {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}
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
			if f.token == "" {
				vars = withoutPhrases(vars)
			}
			raw, _ := json.Marshal(vars)
			p.DynamicVars = string(raw)
		} else {
			p.NoAgent = true
		}
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	if err := fallbackTmpl.Execute(w, p); err != nil {
		log.Printf("call fallback: render: %v", err)
	}
}

var fallbackTmpl = template.Must(template.New("fallback").Parse(`<!doctype html>
<html lang="es">
<head>
  <meta charset="utf-8">
  <meta name="viewport" content="width=device-width, initial-scale=1">
  <meta name="referrer" content="no-referrer">
  {{- if not .Incidents}}
  <meta http-equiv="refresh" content="5">
  {{- end}}
  <title>Aló Mamá: llamada de respaldo</title>
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
