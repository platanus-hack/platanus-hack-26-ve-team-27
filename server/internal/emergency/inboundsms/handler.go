package inboundsms

import (
	"context"
	"errors"
	"log"
	"net/http"

	"github.com/platanus-hack/platanus-hack-26-ve-team-27/server/internal/emergency/ingest"
	"github.com/platanus-hack/platanus-hack-26-ve-team-27/server/internal/emergency/payload"
)

// maxBodyBytes caps the webhook body. The SMS text is 137 characters, but a
// provider posts many other parameters beside it (Twilio sends about 20).
const maxBodyBytes = 16 << 10

// Ingester is the part of ingest.Service the handler needs.
type Ingester interface {
	Ingest(ctx context.Context, signed []byte, ch ingest.Channel, hops int) (ingest.Result, error)
}

// Handler serves POST /sms/inbound/{provider} (#58). The request is the
// provider's own webhook. Responses:
//
//	200 accepted: the ack SMS is sent back through the provider, or no reply
//	    for a message that is permanently unusable (bad text, bad signature,
//	    unknown device, conflicting emergency_id): retrying it cannot help,
//	    and an error status would make the provider retry or alert
//	400 the form body is malformed
//	403 the provider signature is missing or does not verify
//	404 no such provider
//	413 body over 16 KiB
//	503 database not configured, or the provider has no credentials yet
//	500 anything else; the provider may retry
//
// Logs carry the provider and the error, never the sender number or the text.
type Handler struct {
	ing       Ingester // nil: no database
	providers map[string]Provider
}

// New returns the handler. A nil svc means no database: every request to a
// known provider answers 503.
func New(svc *ingest.Service, providers ...Provider) *Handler {
	var ing Ingester // stays a nil interface, not a typed nil pointer
	if svc != nil {
		ing = svc
	}
	return newHandler(ing, providers...)
}

func newHandler(ing Ingester, providers ...Provider) *Handler {
	h := &Handler{ing: ing, providers: make(map[string]Provider, len(providers))}
	for _, p := range providers {
		h.providers[p.Name()] = p
	}
	return h
}

// Register mounts the route on mux.
func (h *Handler) Register(mux *http.ServeMux) {
	mux.HandleFunc("POST /sms/inbound/{provider}", h.handle)
}

func (h *Handler) handle(w http.ResponseWriter, r *http.Request) {
	name := r.PathValue("provider")
	p, ok := h.providers[name]
	if !ok {
		http.Error(w, "unknown provider", http.StatusNotFound)
		return
	}
	if h.ing == nil || !p.Configured() {
		http.Error(w, "not configured", http.StatusServiceUnavailable)
		return
	}
	r.Body = http.MaxBytesReader(w, r.Body, maxBodyBytes)
	msg, err := p.Parse(r)
	if err != nil {
		var tooBig *http.MaxBytesError
		switch {
		case errors.Is(err, ErrUnauthenticated):
			http.Error(w, "invalid signature", http.StatusForbidden)
		case errors.As(err, &tooBig):
			http.Error(w, "body too large", http.StatusRequestEntityTooLarge)
		default:
			http.Error(w, "malformed webhook", http.StatusBadRequest)
		}
		return
	}

	signed, err := payload.ParseSMSText(msg.Body)
	if err == nil {
		var res ingest.Result
		if res, err = h.ing.Ingest(r.Context(), signed, ingest.ChannelSMS, 0); err == nil {
			p.Reply(w, payload.SMSText(res.Ack))
			return
		}
	}
	if permanent(err) {
		log.Printf("inboundsms: %s: message dropped: %v", name, err)
		p.Reply(w, "")
		return
	}
	// A provider that went away cancels the request context: not a server fault.
	if r.Context().Err() == nil {
		log.Printf("inboundsms: %s: ingest: %v", name, err)
	}
	http.Error(w, "internal error", http.StatusInternalServerError)
}

// permanent reports whether retrying the same message can never succeed.
func permanent(err error) bool {
	return errors.Is(err, payload.ErrFormat) || errors.Is(err, payload.ErrSignature) ||
		errors.Is(err, ingest.ErrUnknownDevice) || errors.Is(err, ingest.ErrConflict)
}
