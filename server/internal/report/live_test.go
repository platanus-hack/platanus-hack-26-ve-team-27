package report

import (
	"bufio"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// liveSource is a Source whose record the test changes while a stream runs.
type liveSource struct {
	mu    sync.Mutex
	recs  map[string]Record
	loads atomic.Int32
}

func (s *liveSource) ByToken(_ context.Context, token string) (Record, error) {
	s.loads.Add(1)
	s.mu.Lock()
	defer s.mu.Unlock()
	rec, ok := s.recs[token]
	if !ok {
		return Record{}, ErrNotFound
	}
	return rec, nil
}

func (s *liveSource) Audio(context.Context, string, int) ([]byte, Recording, error) {
	return nil, Recording{}, ErrNotFound
}

func (s *liveSource) set(token string, rec Record) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.recs == nil {
		s.recs = map[string]Record{}
	}
	s.recs[token] = rec
}

func (s *liveSource) drop(token string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.recs, token)
}

// sseMessage is one parsed SSE block: an event with data, or a comment.
type sseMessage struct {
	event, data, id, comment string
}

// readSSE parses the stream into messages until it ends.
func readSSE(body *bufio.Reader, out chan<- sseMessage) {
	defer close(out)
	var m sseMessage
	for {
		line, err := body.ReadString('\n')
		if err != nil {
			return
		}
		line = strings.TrimRight(line, "\n")
		switch {
		case line == "":
			if m != (sseMessage{}) {
				out <- m
			}
			m = sseMessage{}
		case strings.HasPrefix(line, ":"):
			m.comment = strings.TrimSpace(line[1:])
		case strings.HasPrefix(line, "event: "):
			m.event = line[len("event: "):]
		case strings.HasPrefix(line, "data: "):
			m.data = line[len("data: "):]
		case strings.HasPrefix(line, "id: "):
			m.id = line[len("id: "):]
		}
	}
}

// liveServer serves the report routes of src with short stream timings. It
// closes done each time a /live request returns.
func liveServer(t *testing.T, src Source, keepAlive time.Duration) (*httptest.Server, chan struct{}) {
	t.Helper()
	h := NewHandler(src, Options{CanSeal: true, Channel: "WhatsApp"})
	h.LivePoll, h.KeepAlive = 10*time.Millisecond, keepAlive
	h.Now = func() time.Time { return at("2026-10-24T13:11:40Z") } // the fixture's case is live
	mux := http.NewServeMux()
	h.Register(mux)
	done := make(chan struct{}, 4)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mux.ServeHTTP(w, r)
		if strings.HasSuffix(r.URL.Path, "/live") {
			done <- struct{}{}
		}
	}))
	t.Cleanup(srv.Close)
	return srv, done
}

func openStream(t *testing.T, ctx context.Context, url string) (*http.Response, chan sseMessage) {
	t.Helper()
	req, _ := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	msgs := make(chan sseMessage, 16)
	go readSSE(bufio.NewReader(resp.Body), msgs)
	return resp, msgs
}

// next returns the next message that is not a keep-alive comment.
func next(t *testing.T, msgs <-chan sseMessage) sseMessage {
	t.Helper()
	timeout := time.After(3 * time.Second)
	for {
		select {
		case m, ok := <-msgs:
			if !ok {
				return sseMessage{event: "EOF"}
			}
			if m.comment == "" {
				return m
			}
		case <-timeout:
			t.Fatal("no message in 3 s")
		}
	}
}

func decodeSnapshot(t *testing.T, m sseMessage) Snapshot {
	t.Helper()
	if m.event != "update" {
		t.Fatalf("event = %q, want update", m.event)
	}
	var s Snapshot
	if err := json.Unmarshal([]byte(m.data), &s); err != nil {
		t.Fatalf("update data: %v", err)
	}
	if s.Version == "" || m.id != s.Version || len(s.Sections) != len(liveSections) {
		t.Fatalf("snapshot id %q version %q, %d sections", m.id, s.Version, len(s.Sections))
	}
	return s
}

// The stream sends the case at once, then one update per change, in the
// order they happen, and ends after the seal.
func TestLiveSendsEachChangeInOrderAndEndsWhenSealed(t *testing.T) {
	src := &liveSource{}
	rec := openRecord()
	rec.Incident.NotifiedAt = nil
	rec.Calls[0].Verdict, rec.Calls[0].VerdictAt = "", nil
	src.set(testToken, rec)
	srv, done := liveServer(t, src, time.Hour)
	resp, msgs := openStream(t, context.Background(), srv.URL+"/i/"+testToken+"/live")
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK || resp.Header.Get("Content-Type") != "text/event-stream; charset=utf-8" ||
		resp.Header.Get("Cache-Control") != "no-store" || resp.Header.Get("Referrer-Policy") != "no-referrer" {
		t.Fatalf("stream = %d %v", resp.StatusCode, resp.Header)
	}
	first := decodeSnapshot(t, next(t, msgs))
	if !first.Live || first.State != "open" || strings.Contains(first.Sections["timeline"], "Aviso a la familia") ||
		!strings.Contains(first.Sections["latency"], "esperando") {
		t.Fatalf("first snapshot: live %t state %s", first.Live, first.State)
	}

	// The verdict comes first, then the family message: two updates, in order.
	rec.Calls[0].Verdict, rec.Calls[0].VerdictAt, rec.Calls[0].VerdictReason = "duress", ptr(at("2026-10-24T13:11:07Z")), "the user said the duress phrase"
	src.set(testToken, rec)
	second := decodeSnapshot(t, next(t, msgs))
	if second.Version == first.Version || !strings.Contains(second.Sections["timeline"], "Veredicto") || strings.Contains(second.Sections["timeline"], "Aviso a la familia") {
		t.Fatal("second update is not the verdict alone")
	}
	rec.Incident.NotifiedAt = ptr(at("2026-10-24T13:11:08Z"))
	src.set(testToken, rec)
	third := decodeSnapshot(t, next(t, msgs))
	if !strings.Contains(third.Sections["timeline"], "Aviso a la familia") || strings.Contains(third.Sections["latency"], "esperando") {
		t.Fatal("third update lacks the family message")
	}
	if strings.Index(third.Sections["timeline"], "Veredicto") > strings.Index(third.Sections["timeline"], "Aviso a la familia") {
		t.Error("the timeline puts the family message before the verdict")
	}

	src.set(testToken, sealedRecord())
	sealed := decodeSnapshot(t, next(t, msgs))
	if !sealed.End || sealed.Live || sealed.State != "sealed" || !strings.Contains(sealed.Sections["seal"], "Sellada el") {
		t.Fatalf("sealed snapshot: end %t live %t state %s", sealed.End, sealed.Live, sealed.State)
	}
	if m := next(t, msgs); m.event != "end" {
		t.Fatalf("after the seal: %q, want end", m.event)
	}
	if m := next(t, msgs); m.event != "EOF" {
		t.Fatalf("after end: %+v, want the stream closed", m)
	}
	waitDone(t, done)
}

// Nothing changes: only keep-alive comments, no repeated update.
func TestLiveKeepsAliveWithoutRepeating(t *testing.T) {
	src := &liveSource{}
	src.set(testToken, openRecord())
	srv, _ := liveServer(t, src, 20*time.Millisecond)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	resp, msgs := openStream(t, ctx, srv.URL+"/i/"+testToken+"/live")
	defer resp.Body.Close()
	decodeSnapshot(t, next(t, msgs))

	keepAlives := 0
	deadline := time.After(300 * time.Millisecond)
	for done := false; !done; {
		select {
		case m := <-msgs:
			if m.comment == "keep-alive" {
				keepAlives++
			} else {
				t.Fatalf("unchanged case sent %+v", m)
			}
		case <-deadline:
			done = true
		}
	}
	if keepAlives < 3 {
		t.Errorf("%d keep-alives in 300 ms at a 20 ms interval", keepAlives)
	}
}

// A client that goes away ends the handler: it stops reading the case.
func TestLiveStopsWhenTheClientDisconnects(t *testing.T) {
	src := &liveSource{}
	src.set(testToken, openRecord())
	srv, done := liveServer(t, src, time.Hour)
	ctx, cancel := context.WithCancel(context.Background())
	resp, msgs := openStream(t, ctx, srv.URL+"/i/"+testToken+"/live")
	decodeSnapshot(t, next(t, msgs))
	cancel()
	resp.Body.Close()
	waitDone(t, done)
	loads := src.loads.Load()
	time.Sleep(60 * time.Millisecond)
	if src.loads.Load() != loads {
		t.Error("the handler kept reading the case after the client left")
	}
}

// A link that disappears (it expired) ends the stream with gone.
func TestLiveEndsWhenTheLinkIsGone(t *testing.T) {
	src := &liveSource{}
	src.set(testToken, openRecord())
	srv, done := liveServer(t, src, time.Hour)
	resp, msgs := openStream(t, context.Background(), srv.URL+"/i/"+testToken+"/live")
	defer resp.Body.Close()
	decodeSnapshot(t, next(t, msgs))
	src.drop(testToken)
	if m := next(t, msgs); m.event != "end" || !strings.Contains(m.data, "gone") {
		t.Fatalf("after the link went away: %+v", m)
	}
	waitDone(t, done)
}

func TestLiveUnknownTokenIs404AndSealedEndsAtOnce(t *testing.T) {
	src := &liveSource{}
	src.set(testToken, sealedRecord())
	srv, _ := liveServer(t, src, time.Hour)
	resp, err := http.Get(srv.URL + "/i/BBBBBBBBBBBBBBBBBBBBBB/live")
	if err != nil || resp.StatusCode != http.StatusNotFound {
		t.Fatalf("unknown token: %v %v", resp.StatusCode, err)
	}
	resp.Body.Close()

	resp, msgs := openStream(t, context.Background(), srv.URL+"/i/"+testToken+"/live")
	defer resp.Body.Close()
	if s := decodeSnapshot(t, next(t, msgs)); !s.End {
		t.Error("a sealed case is not the end")
	}
	if m := next(t, msgs); m.event != "end" {
		t.Errorf("sealed: %q, want end at once", m.event)
	}
}

// estado.json is the same snapshot, for browsers that cannot stream.
func TestEstadoJSONMatchesTheStream(t *testing.T) {
	src := &liveSource{}
	src.set(testToken, openRecord())
	srv, _ := liveServer(t, src, time.Hour)
	resp, err := http.Get(srv.URL + "/i/" + testToken + "/estado.json")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	var s Snapshot
	if err := json.NewDecoder(resp.Body).Decode(&s); err != nil || resp.Header.Get("Cache-Control") != "no-store" {
		t.Fatalf("estado.json: %v %v", err, resp.Header)
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	stream, msgs := openStream(t, ctx, srv.URL+"/i/"+testToken+"/live")
	defer stream.Body.Close()
	if live := decodeSnapshot(t, next(t, msgs)); live.Version != s.Version {
		t.Errorf("estado.json version %s, stream %s", s.Version, live.Version)
	}
}

func waitDone(t *testing.T, done <-chan struct{}) {
	t.Helper()
	select {
	case <-done:
	case <-time.After(3 * time.Second):
		t.Fatal("the stream handler did not return")
	}
}
