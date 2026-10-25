package report

import (
	"context"
	"encoding/json"
	"io"
	"log"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/platanus-hack/platanus-hack-26-ve-team-27/server/internal/contract"
	"github.com/platanus-hack/platanus-hack-26-ve-team-27/server/internal/duress"
	"github.com/platanus-hack/platanus-hack-26-ve-team-27/server/internal/twiliosig"
)

// TranscriptionPath is where Twilio posts the real-time transcription of a
// call (#143): the call engine gives it as StatusCallbackUrl.
const TranscriptionPath = "/webhooks/twilio/transcription"

// The tracks of a call MAMÁ placed: inbound is the person called ("Tu
// familiar"), outbound is "Mamá".
const (
	TrackInbound  = "inbound_track"
	TrackOutbound = "outbound_track"
)

// LiveTurn is one final phrase of Twilio's real-time transcription of a
// call (call_live_turns).
type LiveTurn struct {
	Track      string // TrackInbound or TrackOutbound
	SequenceID int    // Twilio's SequenceId: ordered per track only
	Message    string
	SaidAt     time.Time // Twilio's Timestamp: orders the two tracks
}

// transcriptTurn is the turn as the page shows it, start being when the
// call was answered.
func (t LiveTurn) transcriptTurn(start time.Time) contract.TranscriptTurn {
	role := duress.RoleUser
	if t.Track == TrackOutbound {
		role = duress.RoleAgent
	}
	return contract.TranscriptTurn{Role: role, Message: t.Message, TimeInCallSecs: max(0, t.SaidAt.Sub(start).Seconds())}
}

// liveStart is when the live turns of the call are counted from: when
// Twilio said it was answered, else when it was requested.
func (c Call) liveStart() time.Time {
	if c.PickedUpAt != nil {
		return *c.PickedUpAt
	}
	return c.RequestedAt
}

// LiveTurnStore is what TranscriptionWebhook writes. DBStore is the real one.
type LiveTurnStore interface {
	SaveLiveTurn(ctx context.Context, callSID string, t LiveTurn) (bool, error)
}

// TranscriptionWebhook handles POST /webhooks/twilio/transcription: the
// events of Twilio's real-time transcription of a call (#143), a form POST
// signed in X-Twilio-Signature. Each final transcription-content is stored
// as a live turn of the call with its CallSid, so the report shows the
// conversation while it happens. Partial results, other events and unknown
// calls are answered 200 and store nothing; transcription-error is only
// logged, and the report then waits for the post-call transcript as before.
// Create it with a struct literal.
type TranscriptionWebhook struct {
	AuthToken string // TWILIO_AUTH_TOKEN; blank answers 503
	// URL is the StatusCallbackUrl the call engine gave Twilio: the URL the
	// signature covers, never one built from the request's Host header.
	URL   string
	Store LiveTurnStore
	Now   func() time.Time // time.Now when nil: the time of a phrase with no Timestamp
}

func (h *TranscriptionWebhook) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	raw, err := io.ReadAll(http.MaxBytesReader(w, r.Body, 64<<10))
	if err != nil {
		http.Error(w, "body too large or unreadable", http.StatusRequestEntityTooLarge)
		return
	}
	if h.AuthToken == "" {
		http.Error(w, "twilio auth token not configured", http.StatusServiceUnavailable)
		return
	}
	params, err := url.ParseQuery(string(raw))
	if err != nil || !twiliosig.Valid(h.AuthToken, h.URL, params, r.Header.Get(twiliosig.Header)) {
		log.Print("twilio transcription: rejected: signature mismatch")
		http.Error(w, "invalid signature", http.StatusForbidden)
		return
	}
	callSID := params.Get("CallSid")
	switch event := params.Get("TranscriptionEvent"); event {
	case "transcription-content":
		turn, ok := liveTurn(params, h.now())
		if !ok {
			break
		}
		if _, err := h.Store.SaveLiveTurn(r.Context(), callSID, turn); err != nil {
			log.Printf("twilio transcription: call %s: store phrase: %v", callSID, err)
			http.Error(w, "store failed", http.StatusInternalServerError)
			return
		}
	case "transcription-started", "transcription-stopped":
		log.Printf("twilio transcription: call %s: %s", callSID, strings.TrimPrefix(event, "transcription-"))
	case "transcription-error":
		log.Printf("twilio transcription: call %s: error %s: %s; the report waits for the post-call transcript",
			callSID, params.Get("TranscriptionErrorCode"), params.Get("TranscriptionError"))
	}
	w.WriteHeader(http.StatusOK)
}

// liveTurn reads a transcription-content event. ok is false for a partial
// result, a track other than the two of a call, or no words.
func liveTurn(p url.Values, now time.Time) (LiveTurn, bool) {
	if !strings.EqualFold(p.Get("Final"), "true") {
		return LiveTurn{}, false
	}
	t := LiveTurn{Track: p.Get("Track")}
	if t.Track != TrackInbound && t.Track != TrackOutbound {
		return LiveTurn{}, false
	}
	var data struct {
		Transcript string `json:"transcript"`
	}
	if err := json.Unmarshal([]byte(p.Get("TranscriptionData")), &data); err != nil {
		return LiveTurn{}, false
	}
	if t.Message = strings.TrimSpace(data.Transcript); t.Message == "" {
		return LiveTurn{}, false
	}
	seq, err := strconv.Atoi(p.Get("SequenceId"))
	if err != nil {
		return LiveTurn{}, false
	}
	t.SequenceID = seq
	if t.SaidAt, err = time.Parse(time.RFC3339Nano, p.Get("Timestamp")); err != nil {
		t.SaidAt = now
	}
	return t, true
}

func (h *TranscriptionWebhook) now() time.Time {
	if h.Now != nil {
		return h.Now()
	}
	return time.Now()
}
