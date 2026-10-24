// Package sms receives offline emergencies texted to the central SMS number
// (#58) and answers each with the signed ack, through the provider the text
// came in on. Each provider is one InboundSMSProvider adapter (Twilio is the
// first); the handler never depends on a provider.
package sms

import (
	"context"
	"encoding/json"
	"errors"
	"log"
	"net/http"

	"github.com/platanus-hack/platanus-hack-26-ve-team-27/server/internal/emergency/ingest"
	"github.com/platanus-hack/platanus-hack-26-ve-team-27/server/internal/emergency/payload"
)

// maxBodyBytes caps the webhook body. A provider's form post is well under it
// even with a 1600-char body.
const maxBodyBytes = 16 << 10

// Provider errors. Parse returns them wrapped.
var (
	// ErrSignature means the webhook signature is missing or does not verify:
	// the request did not come from the provider and is never ingested.
	ErrSignature = errors.New("sms: webhook signature does not verify")
	// ErrFormat means a signed webhook is malformed or misses a field.
	ErrFormat = errors.New("sms: malformed webhook")
)

// InboundSMS is one received text, normalized from the provider's webhook.
type InboundSMS struct {
	From string // sender number, E.164
	Body string // the text as sent
}

// InboundSMSProvider adapts one SMS provider's webhook. Adding a provider is
// adding an implementation; the handler stays the same.
type InboundSMSProvider interface {
	// Parse verifies the webhook signature and returns the text. It wraps
	// ErrSignature for a missing or bad signature, ErrFormat for a malformed
	// signed request, and returns the body read error (an
	// *http.MaxBytesError when over the cap) as is.
	Parse(r *http.Request) (InboundSMS, error)
	// Reply answers the webhook with status 200, texting text back to the
	// sender. An empty text answers without sending anything.
	Reply(w http.ResponseWriter, text string)
}

// Ingester is the part of *ingest.Service the handler uses.
type Ingester interface {
	Ingest(ctx context.Context, signed []byte, ch ingest.Channel, hops int) (ingest.Result, error)
}

var _ Ingester = (*ingest.Service)(nil)

// Handler serves the inbound SMS webhook:
//
//	POST /sms/inbound/{provider}
//
// A text that ingests (new or duplicate) is answered through the provider
// with the signed ack "MAMA1 <b64url>": the duplicate gets it again, which is
// what stops the phone's retries. A permanent failure (not a MAMA text, bad
// payload signature, unknown device, conflict) answers 200 with an empty
// reply, so a stray text to the number gets nothing back and no detail leaks.
// Errors are JSON {"error":"..."}:
//
//	400 signed webhook without the sender or the text
//	403 webhook signature does not verify (never ingested)
//	404 provider unknown or not configured
//	413 body over 16 KiB
//	503 database not configured
//	500 anything else (no ack; the phone retries)
type Handler struct {
	svc       Ingester
	providers map[string]InboundSMSProvider
}

// NewHandler returns the handler. A nil svc means no database: the route
// answers 503. Pass a nil interface then, not a nil *ingest.Service. providers
// maps the {provider} path segment to its adapter.
func NewHandler(svc Ingester, providers map[string]InboundSMSProvider) *Handler {
	return &Handler{svc: svc, providers: providers}
}

// Register mounts the route on mux.
func (h *Handler) Register(mux *http.ServeMux) {
	mux.HandleFunc("POST /sms/inbound/{provider}", h.handleInbound)
}

func (h *Handler) handleInbound(w http.ResponseWriter, r *http.Request) {
	if h.svc == nil {
		writeError(w, http.StatusServiceUnavailable, "database not configured")
		return
	}
	name := r.PathValue("provider")
	p, ok := h.providers[name]
	if !ok {
		writeError(w, http.StatusNotFound, "unknown SMS provider")
		return
	}
	r.Body = http.MaxBytesReader(w, r.Body, maxBodyBytes)
	in, err := p.Parse(r)
	if err != nil {
		writeParseError(w, err)
		return
	}

	signed, err := payload.ParseSMSText(in.Body)
	if err == nil {
		var res ingest.Result
		res, err = h.svc.Ingest(r.Context(), signed, ingest.ChannelSMS, 0)
		if err == nil {
			p.Reply(w, payload.SMSText(res.Ack))
			return
		}
	}
	// Never log the text or the sender: the errors carry neither.
	if permanent(err) {
		log.Printf("sms: dropped a text via %s: %v", name, err)
		p.Reply(w, "")
		return
	}
	// A client that went away cancels the request context: not a server fault.
	if r.Context().Err() == nil {
		log.Printf("sms: ingest via %s: %v", name, err)
	}
	writeError(w, http.StatusInternalServerError, "internal error")
}

// permanent reports whether resending the same text can never succeed.
func permanent(err error) bool {
	return errors.Is(err, payload.ErrFormat) || errors.Is(err, payload.ErrSignature) ||
		errors.Is(err, ingest.ErrUnknownDevice) || errors.Is(err, ingest.ErrConflict)
}

func writeParseError(w http.ResponseWriter, err error) {
	var tooBig *http.MaxBytesError
	switch {
	case errors.As(err, &tooBig):
		writeError(w, http.StatusRequestEntityTooLarge, "body too large")
	case errors.Is(err, ErrSignature):
		writeError(w, http.StatusForbidden, "webhook signature does not verify")
	case errors.Is(err, ErrFormat):
		writeError(w, http.StatusBadRequest, err.Error())
	default:
		writeError(w, http.StatusBadRequest, "cannot read the body")
	}
}

func writeError(w http.ResponseWriter, status int, msg string) {
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(map[string]string{"error": msg})
}
