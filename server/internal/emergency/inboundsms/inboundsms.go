// Package inboundsms receives offline emergencies texted to the central SMS
// number (#58). A Provider (the InboundSMSProvider seam) authenticates one
// SMS vendor's webhook and normalizes it to a Message; Handler feeds the body
// into the #57 ingest pipeline with channel sms and answers with the signed
// ack as an SMS. This file and handler.go know nothing about any vendor:
// switching vendor means adding a Provider, as twilio.go does.
package inboundsms

import (
	"errors"
	"net/http"
)

// ErrUnauthenticated means the request is not a genuine webhook of the
// provider: the signature is missing or does not verify.
var ErrUnauthenticated = errors.New("inboundsms: webhook signature missing or invalid")

// Message is one inbound SMS, normalized.
type Message struct {
	From string // sender number as the provider reports it, usually E.164
	Body string // the SMS text
}

// Provider is the InboundSMSProvider seam: one SMS vendor's webhook.
type Provider interface {
	// Name is the {provider} path segment, for example "twilio".
	Name() string
	// Configured reports whether the credentials the provider needs are set.
	// The handler answers 503 until they are, rather than accept unsigned posts.
	Configured() bool
	// Parse authenticates the webhook request and normalizes it. It returns
	// ErrUnauthenticated for a missing or bad signature. The handler has
	// already capped r.Body; an error from reading it is returned as is.
	Parse(r *http.Request) (Message, error)
	// Reply writes the provider's HTTP response to an accepted webhook. A
	// non-empty text is sent back to the sender as an SMS; empty sends none.
	Reply(w http.ResponseWriter, text string)
}
