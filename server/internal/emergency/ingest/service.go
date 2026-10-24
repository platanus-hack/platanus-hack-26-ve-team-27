package ingest

import (
	"context"
	"errors"
	"fmt"
	"log"
	"time"

	"github.com/platanus-hack/platanus-hack-26-ve-team-27/server/internal/emergency/devicekeys"
	"github.com/platanus-hack/platanus-hack-26-ve-team-27/server/internal/emergency/payload"
	"github.com/platanus-hack/platanus-hack-26-ve-team-27/server/internal/emergency/serverkeys"
)

// EmergencyReceived is published when an emergency is first received, its
// status changes or a newer frame brings a fix (never for duplicates, replays
// or a new channel).
type EmergencyReceived struct {
	EmergencyID string // FormatID
	DeviceID    string // devices.id UUID
	Status      Status
	Location    *Location
	// Channel and Hops describe the receipt that triggered the event; Hops is
	// 0 for http and sms.
	Channel    Channel
	Hops       int
	Seq        uint16
	SentAt     time.Time
	ReceivedAt time.Time
}

// Publisher hands EmergencyReceived on. The server uses
// intake.OfflinePublisher, which opens an incident and notifies the family.
//
// Delivery contract: events are published from a goroutine after the commit,
// so for one emergency they may arrive out of order, and at most once: an
// event is lost if the process crashes or redeploys before it runs, and a
// duplicate receipt is never re-published. Consumers must treat cancelled as
// terminal (never go cancelled to open, whatever order events arrive in), and
// the Kapso consumer needs its own durable claim (for example a notified_at
// column, like incidents) rather than relying on this stream. Not built here.
type Publisher interface {
	Publish(ctx context.Context, ev EmergencyReceived)
}

// LogPublisher only logs, and never the location. Tests use it.
type LogPublisher struct{}

var _ Publisher = LogPublisher{}

// Publish logs the event.
func (LogPublisher) Publish(_ context.Context, ev EmergencyReceived) {
	log.Printf("emergency %s device %s status %s via %s hops %d: Kapso module not wired yet",
		ev.EmergencyID, ev.DeviceID, ev.Status, ev.Channel, ev.Hops)
}

// Result is the outcome of a successful ingest.
type Result struct {
	// Ack is the 76-byte server-signed ack carrying the highest stored seq.
	Ack       []byte
	Emergency Emergency
}

// Service verifies, merges, stores and acks offline emergencies from any
// channel.
type Service struct {
	store Store
	keys  serverkeys.Keys
	pub   Publisher

	// spawn runs a publish without blocking the response. Tests replace it.
	spawn func(func())
	// now is the server clock. Tests replace it.
	now func() time.Time
}

// New returns the ingest service.
func New(store Store, keys serverkeys.Keys, pub Publisher) *Service {
	return &Service{store: store, keys: keys, pub: pub, spawn: func(f func()) { go f() }, now: time.Now}
}

// Ingest verifies one 98-byte signed payload received through ch, merges it
// into the stored emergency and returns the signed ack for the highest seq.
//
// This is the seam the SMS module (#58) uses:
//
//	res, err := svc.Ingest(ctx, signed /* payload.ParseSMSText(body) */, ingest.ChannelSMS, 0)
//
// and it replies payload.SMSText(res.Ack). hops is only read for ChannelMesh
// (pass 0 otherwise); for mesh it must be HopsUnknown or 0..MaxHops. Errors:
// payload.ErrFormat, payload.ErrSignature, ErrUnknownDevice and ErrConflict
// are permanent (do not retry); an unknown Channel or a store failure is a
// plain error. No ack is returned with an error.
func (s *Service) Ingest(ctx context.Context, signed []byte, ch Channel, hops int) (Result, error) {
	switch ch {
	case ChannelHTTP, ChannelSMS:
		hops = 0
	case ChannelMesh:
		if hops != HopsUnknown && (hops < 0 || hops > MaxHops) {
			return Result{}, fmt.Errorf("%w: hops %d out of range", payload.ErrFormat, hops)
		}
	default:
		return Result{}, fmt.Errorf("ingest: unknown channel %q", string(ch))
	}

	p, err := payload.ParseSigned(signed)
	if err != nil {
		return Result{}, err
	}
	dev, err := s.store.Device(ctx, p.DeviceID)
	if errors.Is(err, devicekeys.ErrNotFound) {
		return Result{}, ErrUnknownDevice
	}
	if err != nil {
		return Result{}, err
	}
	if _, err := payload.Verify(signed, dev.PublicKey); err != nil {
		return Result{}, err
	}

	rcpt := Receipt{Payload: p, DeviceID: dev.ID, Channel: ch, Hops: hops}
	var notable bool
	e, err := s.store.Update(ctx, p.EmergencyID, func(prev *Emergency) (Emergency, error) {
		// Taken under the per-id lock, so updated_at never moves backwards
		// when copies of one emergency contend.
		rcpt.ReceivedAt = s.now().UTC()
		next, n, err := merge(prev, rcpt)
		notable = n
		return next, err
	})
	if err != nil {
		return Result{}, err
	}

	if notable {
		s.publish(ctx, EmergencyReceived{
			EmergencyID: FormatID(e.ID),
			DeviceID:    e.DeviceID,
			Status:      e.Status,
			Location:    copyLocation(e.Location),
			Channel:     ch,
			Hops:        hops,
			Seq:         e.Seq,
			SentAt:      e.SentAt,
			ReceivedAt:  rcpt.ReceivedAt,
		})
	}
	return Result{Ack: payload.SignAck(payload.Ack{EmergencyID: e.ID, Seq: e.Seq}, s.keys.Signing), Emergency: e}, nil
}

// IngestMesh opens a relay-uploaded "MAMA1:SEAL:" text with the server sealing
// key and ingests the payload inside it as ChannelMesh. hops is HopsUnknown or
// 0..MaxHops. An ack text, an SMS text or a box sealed to another key fails
// with payload.ErrFormat or payload.ErrDecrypt.
func (s *Service) IngestMesh(ctx context.Context, text string, hops int) (Result, error) {
	frame, err := payload.ParseMeshText(text)
	if err != nil {
		return Result{}, err
	}
	if frame.Type != payload.MeshSealed {
		return Result{}, fmt.Errorf("%w: relay uploads a SEAL text", payload.ErrFormat)
	}
	signed, err := payload.Open(s.keys.Sealing, frame.Bytes)
	if err != nil {
		return Result{}, err
	}
	return s.Ingest(ctx, signed, ChannelMesh, hops)
}

func copyLocation(l *Location) *Location {
	if l == nil {
		return nil
	}
	c := *l
	return &c
}

// publish hands ev to the publisher without blocking the response, like
// intake.handoff: detached from the request context, panics recovered.
func (s *Service) publish(ctx context.Context, ev EmergencyReceived) {
	ctx = context.WithoutCancel(ctx)
	s.spawn(func() {
		defer func() {
			if p := recover(); p != nil {
				log.Printf("ingest: publish of emergency %s panicked: %v", ev.EmergencyID, p)
			}
		}()
		s.pub.Publish(ctx, ev)
	})
}
