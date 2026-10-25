package call

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"log"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/platanus-hack/platanus-hack-26-ve-team-27/server/internal/contract"
	"github.com/platanus-hack/platanus-hack-26-ve-team-27/server/internal/trace"
)

// SignatureHeader carries the HMAC of the post-call webhook:
// "t=<unix secs>,v0=<hex HMAC-SHA256 of "<t>.<raw body>">".
const SignatureHeader = "ElevenLabs-Signature"

// SignatureTolerance is how far the signed timestamp may be from now, the
// window the ElevenLabs docs use.
const SignatureTolerance = 30 * time.Minute

// maxWebhookBytes bounds the body. A transcript is far below it; the
// post_call_audio webhook (base64 audio) can exceed it, so leave audio off.
const maxWebhookBytes = 10 << 20

// Webhook types ElevenLabs posts.
const (
	TypeTranscription         = "post_call_transcription"
	TypeCallInitiationFailure = "call_initiation_failure"
)

var (
	errNoSignature  = errors.New("missing or malformed signature")
	errStale        = errors.New("signature timestamp outside tolerance")
	errBadSignature = errors.New("signature mismatch")
)

// Webhook handles POST /webhooks/elevenlabs. Received is called once per
// valid post_call_transcription; it defaults to logging a summary.
// InitiationFailed is called with a call_initiation_failure webhook, a call
// that never connected; it defaults to logging the reason. The call engine sets both
// (Engine.Received, Engine.InitiationFailed).
type Webhook struct {
	Secret           string // ELEVENLABS_WEBHOOK_SECRET; blank refuses every request
	Received         func(ctx context.Context, hook contract.CallWebhook) error
	InitiationFailed func(ctx context.Context, hook contract.CallWebhook) error
	Now              func() time.Time // time.Now when nil
}

// ServeHTTP verifies the signature over the raw body, then parses it. It
// answers 503 when no secret is configured (fail closed: an unsigned call
// webhook could fake a verdict), 401 on a bad signature, 400 on a bad body,
// 500 when storing it failed (ElevenLabs retries) and 200 otherwise, also
// for webhook types the server does not use, so ElevenLabs does not retry
// them.
func (wh *Webhook) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	raw, err := io.ReadAll(http.MaxBytesReader(w, r.Body, maxWebhookBytes))
	if err != nil {
		http.Error(w, "body too large or unreadable", http.StatusRequestEntityTooLarge)
		return
	}
	if wh.Secret == "" {
		log.Print("call webhook: rejected: ELEVENLABS_WEBHOOK_SECRET is not set")
		trace.Log(unverifiedIncident(raw), "webhook_rejected", "level", "ERROR", "reason", "no_secret",
			"hint", "ELEVENLABS_WEBHOOK_SECRET is not set on this server: every ElevenLabs webhook is refused, so no verdict and no alert")
		http.Error(w, "webhook secret not configured", http.StatusServiceUnavailable)
		return
	}
	if err := verifySignature(r.Header.Get(SignatureHeader), raw, wh.Secret, wh.now()); err != nil {
		log.Printf("call webhook: rejected: %v", err)
		trace.Log(unverifiedIncident(raw), "webhook_rejected", "level", "ERROR", "reason", err,
			"hint", "the HMAC does not match: ELEVENLABS_WEBHOOK_SECRET must be the secret of this webhook in ElevenLabs; the call gets no verdict and no alert")
		http.Error(w, "invalid signature", http.StatusUnauthorized)
		return
	}

	var hook contract.CallWebhook
	if err := json.Unmarshal(raw, &hook); err != nil {
		http.Error(w, "invalid JSON", http.StatusBadRequest)
		return
	}
	switch hook.Type {
	case TypeTranscription:
		if wh.Received == nil {
			LogSummary(hook)
		} else if err := wh.Received(r.Context(), hook); err != nil {
			log.Printf("call webhook: conversation %s: store: %v", hook.Data.ConversationID, err)
			http.Error(w, "could not store the call", http.StatusInternalServerError)
			return
		}
	case TypeCallInitiationFailure:
		reason := logInitiationFailure(raw)
		trace.Log(hook.IncidentID(), "call_failed", "conversation_id", hook.Data.ConversationID, "reason", "elevenlabs_initiation_failure", "detail", reason)
		if wh.InitiationFailed != nil && hook.Data.ConversationID != "" {
			if err := wh.InitiationFailed(r.Context(), hook); err != nil {
				log.Printf("call webhook: conversation %s: mark failed: %v", hook.Data.ConversationID, err)
				http.Error(w, "could not store the call", http.StatusInternalServerError)
				return
			}
		}
	default:
		log.Printf("call webhook: ignoring type %q", hook.Type)
	}
	w.WriteHeader(http.StatusOK)
}

func (wh *Webhook) now() time.Time {
	if wh.Now != nil {
		return wh.Now()
	}
	return time.Now()
}

// verifySignature checks header against body, signed with secret at a
// timestamp within SignatureTolerance of now.
func verifySignature(header string, body []byte, secret string, now time.Time) error {
	var ts, sig string
	for _, part := range strings.Split(header, ",") {
		k, v, ok := strings.Cut(strings.TrimSpace(part), "=")
		if !ok {
			continue
		}
		switch k {
		case "t":
			ts = v
		case "v0":
			sig = v
		}
	}
	if ts == "" || sig == "" {
		return errNoSignature
	}
	secs, err := strconv.ParseInt(ts, 10, 64)
	if err != nil {
		return errNoSignature
	}
	if d := now.Sub(time.Unix(secs, 0)); d > SignatureTolerance || d < -SignatureTolerance {
		return errStale
	}
	got, err := hex.DecodeString(sig)
	if err != nil {
		return errBadSignature
	}
	if !hmac.Equal(got, Sign(secret, ts, body)) {
		return errBadSignature
	}
	return nil
}

// Sign returns HMAC-SHA256(secret, "<ts>.<body>"), the v0 value ElevenLabs
// sends. Exported for tests and the spike's local webhook check.
func Sign(secret, ts string, body []byte) []byte {
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write([]byte(ts + "."))
	mac.Write(body)
	return mac.Sum(nil)
}

// LogSummary logs what the spike measures from a finished call: ids, status,
// duration, turn count and when the agent first spoke. It never logs what
// was said.
func LogSummary(hook contract.CallWebhook) {
	d := hook.Data
	firstAgent := "none"
	for _, t := range d.Transcript {
		if t.Role == "agent" {
			firstAgent = strconv.FormatFloat(t.TimeInCallSecs, 'f', -1, 64) + "s"
			break
		}
	}
	log.Printf("call webhook: conversation %s incident %q status %q duration %ds turns %d first agent turn at %s",
		d.ConversationID, hook.IncidentID(), d.Status, d.Metadata.CallDurationSecs, len(d.Transcript), firstAgent)
}

// logInitiationFailure logs why a call never connected (busy, no answer,
// Twilio refused the number). The spike needs this to tell a blocked +58
// route from an unanswered phone.
func logInitiationFailure(raw []byte) string {
	var f struct {
		Data struct {
			ConversationID string `json:"conversation_id"`
			FailureReason  string `json:"failure_reason"`
		} `json:"data"`
	}
	_ = json.Unmarshal(raw, &f)
	log.Printf("call webhook: call_initiation_failure conversation %s reason %q", f.Data.ConversationID, f.Data.FailureReason)
	return f.Data.FailureReason
}

// unverifiedIncident is the incident_id a rejected webhook claims, for the
// trace only: the body is not trusted, so it is never acted on.
func unverifiedIncident(raw []byte) string {
	var hook contract.CallWebhook
	if json.Unmarshal(raw, &hook) != nil {
		return ""
	}
	return hook.IncidentID()
}
