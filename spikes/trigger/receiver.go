// Command receiver is the test endpoint for spike #10: it stands in for the
// server, logs every request from the phone with the time it arrived, and
// never touches a database. Run it from the repo root:
//
//	go run ./spikes/trigger/receiver.go            # listens on :8089
//	adb -s <serial> reverse tcp:8089 tcp:8089
//
// and build the app with -Pmama.serverUrl=http://127.0.0.1:8089.
package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"time"
)

func main() {
	addr := flag.String("addr", ":8089", "listen address")
	out := flag.String("log", "spike-trigger.log", "append each request as one JSON line here")
	flag.Parse()

	f, err := os.OpenFile(*out, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o644)
	if err != nil {
		log.Fatal(err)
	}
	defer f.Close()

	mux := http.NewServeMux()
	mux.HandleFunc("POST /incidents", record(f, func(w http.ResponseWriter, body []byte) {
		var ev struct {
			IncidentID string `json:"incident_id"`
		}
		_ = json.Unmarshal(body, &ev)
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusCreated)
		// The spike does not pick a branch; "call" is the unknown-speed answer.
		_ = json.NewEncoder(w).Encode(map[string]string{"incident_id": ev.IncidentID, "branch": "call"})
	}))
	mux.HandleFunc("POST /incidents/{id}/points", record(f, noContent))
	mux.HandleFunc("POST /devices", record(f, noContent))
	mux.HandleFunc("POST /spike/note", record(f, noContent))
	// The phone's warm-up GET; the real server answers GET / too.
	mux.HandleFunc("GET /{$}", func(w http.ResponseWriter, _ *http.Request) { _, _ = w.Write([]byte("spike receiver\n")) })
	// The camera probe's mp4, saved next to the log.
	mux.HandleFunc("POST /spike/video", saveVideo(f, filepath.Dir(*out)))

	log.Printf("spike receiver on %s, logging to %s", *addr, *out)
	log.Fatal(http.ListenAndServe(*addr, mux))
}

func noContent(w http.ResponseWriter, _ []byte) { w.WriteHeader(http.StatusNoContent) }

// record logs the arrival time, the path, the phone's own since-trigger
// header and the body, then lets reply answer.
func record(f *os.File, reply func(http.ResponseWriter, []byte)) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		received := time.Now().UTC()
		body, err := io.ReadAll(io.LimitReader(r.Body, 1<<20))
		if err != nil {
			http.Error(w, "read body", http.StatusBadRequest)
			return
		}
		entry := map[string]any{
			"received_at":          received.Format(time.RFC3339Nano),
			"path":                 r.URL.Path,
			"since_trigger_ms_hdr": r.Header.Get("X-Spike-Since-Trigger-Ms"),
			"via":                  r.Header.Get("X-Spike-Path"),
		}
		var parsed map[string]any
		if json.Unmarshal(body, &parsed) == nil {
			entry["body"] = parsed
			// Arrival minus triggered_at, both wall clocks: correct by the
			// phone-laptop clock offset before reading it as latency.
			if s, ok := parsed["triggered_at"].(string); ok {
				if t, err := time.Parse(time.RFC3339Nano, s); err == nil {
					entry["wall_delta_ms"] = received.Sub(t).Milliseconds()
				}
			}
		} else {
			entry["raw"] = string(body)
		}
		line, _ := json.Marshal(entry)
		fmt.Fprintln(f, string(line))
		log.Println(string(line))
		reply(w, body)
	}
}

var safeID = regexp.MustCompile(`^[0-9a-f-]{1,64}$`)

// saveVideo stores the raw body as spike-video-<incident_id>.mp4 in dir and
// logs its size and arrival time.
func saveVideo(f *os.File, dir string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		received := time.Now().UTC()
		id := r.URL.Query().Get("incident_id")
		if !safeID.MatchString(id) {
			http.Error(w, "bad incident_id", http.StatusBadRequest)
			return
		}
		body, err := io.ReadAll(http.MaxBytesReader(w, r.Body, 50<<20))
		if err != nil {
			http.Error(w, "body too large or unreadable", http.StatusRequestEntityTooLarge)
			return
		}
		path := filepath.Join(dir, "spike-video-"+id+".mp4")
		if err := os.WriteFile(path, body, 0o644); err != nil {
			http.Error(w, "write", http.StatusInternalServerError)
			return
		}
		line, _ := json.Marshal(map[string]any{
			"received_at":          received.Format(time.RFC3339Nano),
			"path":                 r.URL.Path,
			"incident_id":          id,
			"bytes":                len(body),
			"saved":                path,
			"since_trigger_ms_hdr": r.Header.Get("X-Spike-Since-Trigger-Ms"),
		})
		fmt.Fprintln(f, string(line))
		log.Println(string(line))
		w.WriteHeader(http.StatusNoContent)
	}
}
