package ingest

import (
	"errors"
	"testing"
	"time"

	"github.com/platanus-hack/platanus-hack-26-ve-team-27/server/internal/emergency/payload"
)

const testDevice = "3f1c9a52-7b1e-4c8a-9a0e-2d6b8f4e1a77"

func receipt(kind payload.Kind, seq uint16, fix bool, ch Channel, hops int, at time.Time) Receipt {
	p := payload.Payload{
		Kind:        kind,
		EmergencyID: 0x0123456789abcdef,
		Seq:         seq,
		LocAgeS:     payload.NoLocation,
		Timestamp:   1792836000 + uint32(seq),
	}
	if fix {
		p.LatE5, p.LonE5, p.LocAgeS = 1048801, -6687919, 42
	}
	return Receipt{Payload: p, DeviceID: testDevice, Channel: ch, Hops: hops, ReceivedAt: at}
}

var t0 = time.Date(2026, 10, 24, 12, 0, 0, 0, time.UTC)

func mustMerge(t *testing.T, prev *Emergency, r Receipt) (Emergency, bool) {
	t.Helper()
	e, notable, err := merge(prev, r)
	if err != nil {
		t.Fatalf("merge: %v", err)
	}
	return e, notable
}

func TestFormatID(t *testing.T) {
	for id, want := range map[uint64]string{
		0x0123456789abcdef: "0123456789abcdef",
		0xfedcba9876543210: "fedcba9876543210",
		1:                  "0000000000000001",
	} {
		if got := FormatID(id); got != want {
			t.Errorf("FormatID(%x) = %q, want %q", id, got, want)
		}
	}
}

func TestMergeNew(t *testing.T) {
	e, notable := mustMerge(t, nil, receipt(payload.KindAlert, 0, true, ChannelMesh, 2, t0))
	if !notable || e.Status != StatusOpen || e.Seq != 0 || e.MinHops != 2 {
		t.Fatalf("got %+v notable=%v", e, notable)
	}
	if len(e.Channels) != 1 || e.Channels[0] != ChannelMesh {
		t.Errorf("channels = %v", e.Channels)
	}
	if e.Location == nil || e.Location.LatE5 != 1048801 {
		t.Errorf("location = %+v", e.Location)
	}
	if !e.FirstReceivedAt.Equal(t0) || !e.UpdatedAt.Equal(t0) || e.CancelledAt != nil {
		t.Errorf("times: %+v", e)
	}
	if e.ID != 0x0123456789abcdef || e.DeviceID != testDevice {
		t.Errorf("identity: %+v", e)
	}

	c, notable := mustMerge(t, nil, receipt(payload.KindCancel, 3, false, ChannelHTTP, 0, t0))
	if !notable || c.Status != StatusCancelled || c.CancelledAt == nil || !c.CancelledAt.Equal(t0) {
		t.Errorf("cancel first: %+v", c)
	}
	if c.Location != nil {
		t.Errorf("no-fix cancel has location %+v", c.Location)
	}

	h, _ := mustMerge(t, nil, receipt(payload.KindAlert, 0, true, ChannelHTTP, 0, t0))
	if h.MinHops != HopsUnknown {
		t.Errorf("http MinHops = %d", h.MinHops)
	}
	m, _ := mustMerge(t, nil, receipt(payload.KindAlert, 0, true, ChannelMesh, HopsUnknown, t0))
	if m.MinHops != HopsUnknown {
		t.Errorf("mesh unknown MinHops = %d", m.MinHops)
	}
}

func TestMergeDuplicateViaOtherChannel(t *testing.T) {
	first, _ := mustMerge(t, nil, receipt(payload.KindAlert, 0, true, ChannelMesh, 3, t0))
	t1 := t0.Add(time.Minute)
	e, notable := mustMerge(t, &first, receipt(payload.KindAlert, 0, true, ChannelHTTP, 0, t1))
	if notable || len(e.Channels) != 2 || e.Channels[0] != ChannelMesh || e.Channels[1] != ChannelHTTP {
		t.Fatalf("got %+v notable=%v", e, notable)
	}
	if e.Seq != 0 || !e.UpdatedAt.Equal(t1) || !e.FirstReceivedAt.Equal(t0) {
		t.Errorf("times/seq: %+v", e)
	}
	e2, notable := mustMerge(t, &e, receipt(payload.KindAlert, 0, true, ChannelHTTP, 0, t1.Add(time.Second)))
	if notable || len(e2.Channels) != 2 {
		t.Errorf("repeat: %+v", e2)
	}
	if len(first.Channels) != 1 {
		t.Errorf("prev channels aliased: %v", first.Channels)
	}

	h1, _ := mustMerge(t, &first, receipt(payload.KindAlert, 0, true, ChannelMesh, 1, t1))
	if h1.MinHops != 1 {
		t.Errorf("MinHops = %d, want 1", h1.MinHops)
	}
	h5, _ := mustMerge(t, &h1, receipt(payload.KindAlert, 0, true, ChannelMesh, 5, t1))
	if h5.MinHops != 1 {
		t.Errorf("MinHops = %d, want 1", h5.MinHops)
	}
}

func TestMergeHigherSeqUpdatesLocation(t *testing.T) {
	a, _ := mustMerge(t, nil, receipt(payload.KindAlert, 0, true, ChannelHTTP, 0, t0))
	r := receipt(payload.KindUpdate, 1, true, ChannelHTTP, 0, t0.Add(time.Second))
	r.Payload.LatE5 = 1048900
	e, notable := mustMerge(t, &a, r)
	if notable || e.Seq != 1 || e.Location.LatE5 != 1048900 {
		t.Fatalf("got %+v notable=%v", e, notable)
	}
	if want := time.Unix(int64(r.Payload.Timestamp), 0).UTC(); !e.SentAt.Equal(want) {
		t.Errorf("SentAt = %v, want %v", e.SentAt, want)
	}
	if a.Location.LatE5 != 1048801 {
		t.Errorf("prev location mutated")
	}
}

func TestMergeNoFixKeepsLocation(t *testing.T) {
	a, _ := mustMerge(t, nil, receipt(payload.KindAlert, 0, true, ChannelHTTP, 0, t0))
	e, _ := mustMerge(t, &a, receipt(payload.KindUpdate, 1, false, ChannelHTTP, 0, t0.Add(time.Second)))
	if e.Seq != 1 || e.Location == nil || *e.Location != *a.Location {
		t.Errorf("location lost: %+v", e)
	}
	if !e.SentAt.After(a.SentAt) {
		t.Errorf("SentAt not advanced")
	}
}

func TestMergeOutOfOrder(t *testing.T) {
	u := receipt(payload.KindUpdate, 1, true, ChannelHTTP, 0, t0)
	u.Payload.LatE5 = 1048900
	e, _ := mustMerge(t, nil, u)
	e2, notable := mustMerge(t, &e, receipt(payload.KindAlert, 0, true, ChannelMesh, 1, t0.Add(time.Second)))
	if notable || e2.Seq != 1 || e2.Location.LatE5 != 1048900 {
		t.Errorf("stale alert moved state: %+v", e2)
	}
	c, _ := mustMerge(t, &e2, receipt(payload.KindCancel, 2, false, ChannelHTTP, 0, t0.Add(2*time.Second)))
	e3, _ := mustMerge(t, &c, receipt(payload.KindUpdate, 1, true, ChannelMesh, 1, t0.Add(3*time.Second)))
	if e3.Status != StatusCancelled || e3.Seq != 2 {
		t.Errorf("after cancel: %+v", e3)
	}
}

func TestMergeCancel(t *testing.T) {
	a, _ := mustMerge(t, nil, receipt(payload.KindAlert, 0, true, ChannelHTTP, 0, t0))
	o, _ := mustMerge(t, &a, receipt(payload.KindUpdate, 1, true, ChannelHTTP, 0, t0))
	c, notable := mustMerge(t, &o, receipt(payload.KindCancel, 2, false, ChannelHTTP, 0, t0.Add(time.Minute)))
	if !notable || c.Status != StatusCancelled || c.CancelledAt == nil {
		t.Fatalf("cancel: %+v notable=%v", c, notable)
	}
	first := *c.CancelledAt
	c2, notable := mustMerge(t, &c, receipt(payload.KindCancel, 2, false, ChannelMesh, 1, t0.Add(time.Hour)))
	if notable || !c2.CancelledAt.Equal(first) {
		t.Errorf("repeat cancel: %+v notable=%v", c2, notable)
	}

	s3 := receipt(payload.KindUpdate, 3, true, ChannelHTTP, 0, t0)
	three, _ := mustMerge(t, nil, s3)
	lc, notable := mustMerge(t, &three, receipt(payload.KindCancel, 2, false, ChannelHTTP, 0, t0.Add(time.Minute)))
	if !notable || lc.Status != StatusCancelled || lc.Seq != 3 {
		t.Errorf("lower-seq cancel: %+v", lc)
	}

	acked := o
	acked.Status = StatusAcked
	ac, notable := mustMerge(t, &acked, receipt(payload.KindCancel, 2, false, ChannelHTTP, 0, t0.Add(time.Minute)))
	if !notable || ac.Status != StatusCancelled {
		t.Errorf("acked cancel: %+v", ac)
	}
}

func TestMergeConflict(t *testing.T) {
	a, _ := mustMerge(t, nil, receipt(payload.KindAlert, 0, true, ChannelHTTP, 0, t0))
	a.DeviceID = "11111111-1111-4111-8111-111111111111"
	_, _, err := merge(&a, receipt(payload.KindAlert, 0, true, ChannelHTTP, 0, t0))
	if !errors.Is(err, ErrConflict) {
		t.Errorf("err = %v", err)
	}
}
