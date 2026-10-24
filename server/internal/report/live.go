package report

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"net/http"
	"time"
)

// Live stream timing defaults (Handler fields left zero).
const (
	// defaultLivePoll is how often the stream re-reads the case. A few small
	// queries a second per open page is nothing at this scale, and needs no
	// LISTEN/NOTIFY.
	defaultLivePoll = time.Second
	// defaultKeepAlive is how often an idle stream sends a comment, so
	// Render's proxy does not close it.
	defaultKeepAlive = 15 * time.Second
	// defaultMaxStream bounds one stream; EventSource then reconnects.
	defaultMaxStream = 30 * time.Minute
)

// liveSections are the page parts the live stream sends, each rendered by
// the "sec-<name>" template into the [data-live="<name>"] container.
var liveSections = []string{"hero", "latency", "timeline", "map", "points", "calls", "emergencies", "seal"}

// Snapshot is one state of the page for the live stream and estado.json.
type Snapshot struct {
	Version   string            `json:"version"` // changes when any section changes
	State     string            `json:"state"`   // open, sealed or unsealed
	Live      bool              `json:"live"`    // not sealed yet
	End       bool              `json:"end"`     // sealed: nothing will change any more
	ServerNow string            `json:"server_now"`
	Sections  map[string]string `json:"sections"`
}

// snapshot renders the record's live sections.
func (h *Handler) snapshot(rec Record, token string) (Snapshot, error) {
	opts := h.opts
	opts.Token = token
	opts.Now = h.now()
	page := Build(rec, opts)
	s := Snapshot{
		State: page.State, Live: page.Live, End: rec.Seal != nil,
		ServerNow: iso(opts.Now), Sections: make(map[string]string, len(liveSections)),
	}
	sum := sha256.New()
	for _, name := range liveSections {
		var buf bytes.Buffer
		if err := tmpl.ExecuteTemplate(&buf, "sec-"+name, page); err != nil {
			return Snapshot{}, fmt.Errorf("render %s: %w", name, err)
		}
		s.Sections[name] = buf.String()
		fmt.Fprintf(sum, "%s\x00%d\x00", name, buf.Len())
		sum.Write(buf.Bytes())
	}
	s.Version = hex.EncodeToString(sum.Sum(nil))[:16]
	return s, nil
}

// state answers GET /i/{token}/estado.json: the same snapshot the stream
// sends, for a browser that cannot keep an EventSource open.
func (h *Handler) state(w http.ResponseWriter, r *http.Request) {
	rec, token, ok := h.load(w, r)
	if !ok {
		return
	}
	h.closer.Recover(rec)
	s, err := h.snapshot(rec, token)
	if err != nil {
		log.Printf("report: estado: %v", err)
		http.Error(w, "No se pudo cargar la ficha.", http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(s)
}

// live answers GET /i/{token}/live with Server-Sent Events: an "update"
// event with the snapshot at once, then one each time the case changes (it
// re-reads it every LivePoll), a ": keep-alive" comment every KeepAlive, and
// an "end" event once the case is sealed or its link is gone, after which
// the stream closes. It also stops when the client goes away, and after
// MaxStream (EventSource reconnects by itself).
func (h *Handler) live(w http.ResponseWriter, r *http.Request) {
	rec, token, ok := h.load(w, r)
	if !ok {
		return
	}
	rc := http.NewResponseController(w)
	_ = rc.SetWriteDeadline(time.Time{}) // a stream outlives any server write timeout
	hdr := w.Header()
	hdr.Set("Content-Type", "text/event-stream; charset=utf-8")
	hdr.Set("X-Accel-Buffering", "no")
	w.WriteHeader(http.StatusOK)

	ctx := r.Context()
	write := func(s string) bool {
		if _, err := fmt.Fprint(w, s); err != nil {
			return false
		}
		return rc.Flush() == nil
	}
	last := ""
	// push sends the snapshot when it changed and reports whether the
	// stream must stop (sealed, or the client is gone).
	push := func(rec Record) (stop bool) {
		h.closer.Recover(rec)
		s, err := h.snapshot(rec, token)
		if err != nil {
			log.Printf("report: live: %v", err)
			return false
		}
		if s.Version != last {
			data, _ := json.Marshal(s)
			if !write("id: " + s.Version + "\nevent: update\ndata: " + string(data) + "\n\n") {
				return true
			}
			last = s.Version
		}
		if s.End {
			write("event: end\ndata: {}\n\n")
			return true
		}
		return false
	}

	if !write("retry: 3000\n\n") || push(rec) {
		return
	}
	poll := time.NewTicker(orDefault(h.LivePoll, defaultLivePoll))
	defer poll.Stop()
	keep := time.NewTicker(orDefault(h.KeepAlive, defaultKeepAlive))
	defer keep.Stop()
	deadline := time.NewTimer(orDefault(h.MaxStream, defaultMaxStream))
	defer deadline.Stop()
	loadErrLogged := false
	for {
		select {
		case <-ctx.Done():
			return
		case <-deadline.C:
			return
		case <-keep.C:
			if !write(": keep-alive\n\n") {
				return
			}
		case <-poll.C:
			rec, err := h.src.ByToken(ctx, token)
			if errors.Is(err, ErrNotFound) {
				write("event: end\ndata: {\"gone\":true}\n\n")
				return
			}
			if err != nil {
				if !loadErrLogged && !errors.Is(err, context.Canceled) {
					log.Printf("report: live: load: %v", err)
					loadErrLogged = true
				}
				continue
			}
			if push(rec) {
				return
			}
		}
	}
}

func orDefault(d, def time.Duration) time.Duration {
	if d <= 0 {
		return def
	}
	return d
}
