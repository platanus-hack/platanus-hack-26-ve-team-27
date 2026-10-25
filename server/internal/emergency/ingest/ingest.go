// Package ingest receives offline emergencies (#57): verify, dedup by
// emergency_id, store, ack, publish EmergencyReceived; see
// docs/adr/0003-offline-emergencies-store.md.
package ingest

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"time"

	"github.com/platanus-hack/platanus-hack-26-ve-team-27/server/internal/emergency/devicekeys"
	"github.com/platanus-hack/platanus-hack-26-ve-team-27/server/internal/emergency/payload"
)

// Channel is how a receipt reached the server.
type Channel string

const (
	ChannelHTTP Channel = "http"
	ChannelSMS  Channel = "sms"
	ChannelMesh Channel = "mesh"
)

// Status is the lifecycle of an emergency. StatusAcked is reserved for "a
// family contact acknowledged it" (Kapso module, later ticket); #57 never sets
// it. The only transition #57 makes is any status to cancelled.
type Status string

const (
	StatusOpen      Status = "open"
	StatusAcked     Status = "acked"
	StatusCancelled Status = "cancelled"
)

const (
	// HopsUnknown means the mesh hop count is not known.
	HopsUnknown = -1
	// MaxHops is the largest accepted mesh hop count.
	MaxHops = 255
)

// FormatID is the 16 lowercase hex digits of an emergency_id (the DB key and
// the event id).
func FormatID(id uint64) string { return fmt.Sprintf("%016x", id) }

// Location is the last known fix.
type Location struct {
	LatE5, LonE5 int32
	AgeS         uint16
	// SentAt is the timestamp of the payload that carried the fix.
	SentAt time.Time
}

// Lat is the latitude in degrees.
func (l Location) Lat() float64 { return float64(l.LatE5) / 1e5 }

// Lon is the longitude in degrees.
func (l Location) Lon() float64 { return float64(l.LonE5) / 1e5 }

// Emergency is the stored record of one emergency_id.
type Emergency struct {
	ID       uint64
	DeviceID string // devices.id UUID
	Status   Status
	Seq      uint16    // highest accepted
	SentAt   time.Time // payload timestamp of the highest seq
	Location *Location
	Channels []Channel // first-seen order
	// MinHops is the smallest known mesh hop count, HopsUnknown if none.
	MinHops         int
	FirstReceivedAt time.Time
	UpdatedAt       time.Time
	CancelledAt     *time.Time
}

// Receipt is one verified payload arriving through a channel.
type Receipt struct {
	Payload    payload.Payload
	DeviceID   string
	Channel    Channel
	Hops       int
	ReceivedAt time.Time
}

var (
	// ErrConflict means the emergency_id already belongs to another device.
	ErrConflict = errors.New("ingest: emergency_id belongs to another device")
	// ErrUnknownDevice means the signing device is not registered.
	ErrUnknownDevice = errors.New("ingest: device not registered")
)

// Store persists emergencies. Update runs apply once with the stored record
// (nil if none) while holding a per-id lock, so concurrent calls on one id run
// one after the other; an apply error saves nothing and is returned as is;
// otherwise the returned record is inserted or overwritten and returned.
type Store interface {
	Device(ctx context.Context, keyID uint64) (devicekeys.Device, error)
	Update(ctx context.Context, emergencyID uint64, apply func(prev *Emergency) (Emergency, error)) (Emergency, error)
}

func payloadTime(p payload.Payload) time.Time { return time.Unix(int64(p.Timestamp), 0).UTC() }

func hasFix(p payload.Payload) bool { return p.LocAgeS != payload.NoLocation }

func fixOf(p payload.Payload) *Location {
	return &Location{LatE5: p.LatE5, LonE5: p.LonE5, AgeS: p.LocAgeS, SentAt: payloadTime(p)}
}

// knownHops returns the receipt's mesh hop count and whether it is known.
func knownHops(r Receipt) (int, bool) {
	return r.Hops, r.Channel == ChannelMesh && r.Hops != HopsUnknown
}

// merge applies one receipt to the stored record (nil if none). notable is
// true for a new record, a status change or a newer frame with a fix: those
// are published.
func merge(prev *Emergency, r Receipt) (next Emergency, notable bool, err error) {
	p := r.Payload
	cancel := p.Kind == payload.KindCancel

	if prev == nil {
		next = Emergency{
			ID:              p.EmergencyID,
			DeviceID:        r.DeviceID,
			Status:          StatusOpen,
			Seq:             p.Seq,
			SentAt:          payloadTime(p),
			Channels:        []Channel{r.Channel},
			MinHops:         HopsUnknown,
			FirstReceivedAt: r.ReceivedAt,
			UpdatedAt:       r.ReceivedAt,
		}
		if hasFix(p) {
			next.Location = fixOf(p)
		}
		if h, ok := knownHops(r); ok {
			next.MinHops = h
		}
		if cancel {
			at := r.ReceivedAt
			next.Status, next.CancelledAt = StatusCancelled, &at
		}
		return next, true, nil
	}

	if prev.DeviceID != r.DeviceID {
		return Emergency{}, false, ErrConflict
	}

	next = *prev
	next.Channels = slices.Clone(prev.Channels)
	if prev.Location != nil {
		loc := *prev.Location
		next.Location = &loc
	}
	if prev.CancelledAt != nil {
		at := *prev.CancelledAt
		next.CancelledAt = &at
	}
	if !slices.Contains(next.Channels, r.Channel) {
		next.Channels = append(next.Channels, r.Channel)
	}
	if h, ok := knownHops(r); ok && (next.MinHops == HopsUnknown || h < next.MinHops) {
		next.MinHops = h
	}
	next.UpdatedAt = r.ReceivedAt

	if p.Seq > prev.Seq {
		next.Seq = p.Seq
		next.SentAt = payloadTime(p)
		if hasFix(p) {
			next.Location = fixOf(p)
			// The incident (intake.OfflinePublisher) gets the new fix as a GPS point.
			notable = prev.Status != StatusCancelled
		}
	}
	if cancel && prev.Status != StatusCancelled {
		at := r.ReceivedAt
		next.Status, next.CancelledAt = StatusCancelled, &at
		notable = true
	}
	return next, notable, nil
}
