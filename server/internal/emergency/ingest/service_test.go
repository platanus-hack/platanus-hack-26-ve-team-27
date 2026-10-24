package ingest

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"crypto/sha256"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/platanus-hack/platanus-hack-26-ve-team-27/server/internal/emergency/devicekeys"
	"github.com/platanus-hack/platanus-hack-26-ve-team-27/server/internal/emergency/payload"
	"github.com/platanus-hack/platanus-hack-26-ve-team-27/server/internal/emergency/serverkeys"
)

const svcDeviceUUID = "11111111-2222-4333-8444-555555555555"

type fakeStore struct {
	mu      sync.Mutex
	records map[uint64]Emergency
	devices map[uint64]devicekeys.Device
}

func newFakeStore() *fakeStore {
	return &fakeStore{records: map[uint64]Emergency{}, devices: map[uint64]devicekeys.Device{}}
}

func (s *fakeStore) addDevice(id string, pub ed25519.PublicKey) {
	s.devices[payload.DeviceID(pub)] = devicekeys.Device{ID: id, PublicKey: pub}
}

func (s *fakeStore) Device(_ context.Context, keyID uint64) (devicekeys.Device, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	d, ok := s.devices[keyID]
	if !ok {
		return devicekeys.Device{}, devicekeys.ErrNotFound
	}
	return d, nil
}

func (s *fakeStore) Update(_ context.Context, id uint64, apply func(*Emergency) (Emergency, error)) (Emergency, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	var prev *Emergency
	if e, ok := s.records[id]; ok {
		prev = &e
	}
	next, err := apply(prev)
	if err != nil {
		return Emergency{}, err
	}
	s.records[id] = next
	return next, nil
}

type fakePublisher struct {
	mu     sync.Mutex
	events []EmergencyReceived
}

func (p *fakePublisher) Publish(_ context.Context, ev EmergencyReceived) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.events = append(p.events, ev)
}

func (p *fakePublisher) all() []EmergencyReceived {
	p.mu.Lock()
	defer p.mu.Unlock()
	return append([]EmergencyReceived(nil), p.events...)
}

const svcEmergencyID = 0xfedcba9876543210

type fixture struct {
	t     *testing.T
	store *fakeStore
	pub   *fakePublisher
	keys  serverkeys.Keys
	dev   ed25519.PrivateKey
	svc   *Service
}

func newFixture(t *testing.T) *fixture {
	t.Helper()
	seed := sha256.Sum256([]byte("ingest-test-device"))
	dev := ed25519.NewKeyFromSeed(seed[:])
	keys, err := serverkeys.Generate(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	store := newFakeStore()
	store.addDevice(svcDeviceUUID, dev.Public().(ed25519.PublicKey))
	pub := &fakePublisher{}
	svc := New(store, keys, pub)
	svc.spawn = func(f func()) { f() }
	svc.now = func() time.Time { return t0 }
	return &fixture{t: t, store: store, pub: pub, keys: keys, dev: dev, svc: svc}
}

func (f *fixture) signedWith(key ed25519.PrivateKey, kind payload.Kind, seq uint16, fix bool) []byte {
	f.t.Helper()
	p := payload.Payload{
		Kind:        kind,
		DeviceID:    payload.DeviceID(key.Public().(ed25519.PublicKey)),
		EmergencyID: svcEmergencyID,
		Seq:         seq,
		LocAgeS:     payload.NoLocation,
		Timestamp:   1792836000 + uint32(seq),
	}
	if fix {
		p.LatE5, p.LonE5, p.LocAgeS = 1048801, -6687919, 42
	}
	b, err := payload.Sign(p, key)
	if err != nil {
		f.t.Fatal(err)
	}
	return b
}

func (f *fixture) signed(kind payload.Kind, seq uint16, fix bool) []byte {
	return f.signedWith(f.dev, kind, seq, fix)
}

func (f *fixture) sealedTo(k serverkeys.Keys, signed []byte) string {
	f.t.Helper()
	box, err := payload.Seal(rand.Reader, k.Sealing.PublicKey(), signed)
	if err != nil {
		f.t.Fatal(err)
	}
	return payload.MeshSealedText(box)
}

func (f *fixture) sealed(signed []byte) string { return f.sealedTo(f.keys, signed) }

func (f *fixture) mustAck(res Result, seq uint16) {
	f.t.Helper()
	a, err := payload.VerifyAck(res.Ack, f.keys.SigningPublic())
	if err != nil {
		f.t.Fatal(err)
	}
	if a != (payload.Ack{EmergencyID: svcEmergencyID, Seq: seq}) {
		f.t.Fatalf("ack = %+v, want seq %d", a, seq)
	}
}

func TestRelayAndHTTPMakeOneRecord(t *testing.T) {
	f := newFixture(t)
	alert := f.signed(payload.KindAlert, 0, true)
	if _, err := f.svc.IngestMesh(context.Background(), f.sealed(alert), 2); err != nil {
		t.Fatal(err)
	}
	if _, err := f.svc.Ingest(context.Background(), alert, ChannelHTTP, 0); err != nil {
		t.Fatal(err)
	}
	if len(f.store.records) != 1 {
		t.Fatalf("records = %d", len(f.store.records))
	}
	e := f.store.records[svcEmergencyID]
	if len(e.Channels) != 2 || e.Channels[0] != ChannelMesh || e.Channels[1] != ChannelHTTP || e.MinHops != 2 {
		t.Fatalf("record = %+v", e)
	}
	evs := f.pub.all()
	if len(evs) != 1 {
		t.Fatalf("events = %d", len(evs))
	}
	ev := evs[0]
	if ev.Status != StatusOpen || ev.Channel != ChannelMesh || ev.Hops != 2 || ev.EmergencyID != FormatID(svcEmergencyID) ||
		ev.DeviceID != svcDeviceUUID || ev.Location == nil {
		t.Fatalf("event = %+v", ev)
	}
}

func TestAckVerifiesWithServerKey(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	res, err := f.svc.Ingest(ctx, f.signed(payload.KindAlert, 0, true), ChannelHTTP, 0)
	if err != nil {
		t.Fatal(err)
	}
	f.mustAck(res, 0)
	if res, err = f.svc.Ingest(ctx, f.signed(payload.KindUpdate, 3, true), ChannelHTTP, 0); err != nil {
		t.Fatal(err)
	}
	f.mustAck(res, 3)
	if res, err = f.svc.Ingest(ctx, f.signed(payload.KindUpdate, 1, true), ChannelHTTP, 0); err != nil {
		t.Fatal(err)
	}
	f.mustAck(res, 3)
}

func TestInvalidSignatureIsRejected(t *testing.T) {
	f := newFixture(t)
	b := f.signed(payload.KindAlert, 0, true)
	b[len(b)-1] ^= 1
	_, err := f.svc.Ingest(context.Background(), b, ChannelHTTP, 0)
	if !errors.Is(err, payload.ErrSignature) {
		t.Fatalf("err = %v", err)
	}
	if len(f.store.records) != 0 || len(f.pub.all()) != 0 {
		t.Fatal("stored or published")
	}

	seed := sha256.Sum256([]byte("ingest-test-stranger"))
	other := ed25519.NewKeyFromSeed(seed[:])
	_, err = f.svc.Ingest(context.Background(), f.signedWith(other, payload.KindAlert, 0, true), ChannelHTTP, 0)
	if !errors.Is(err, ErrUnknownDevice) {
		t.Fatalf("err = %v", err)
	}
	if len(f.store.records) != 0 || len(f.pub.all()) != 0 {
		t.Fatal("stored or published")
	}
}

func TestMalformedIsFormatError(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	if _, err := f.svc.Ingest(ctx, make([]byte, 97), ChannelHTTP, 0); !errors.Is(err, payload.ErrFormat) {
		t.Fatalf("97 bytes: %v", err)
	}
	ack := payload.SignAck(payload.Ack{EmergencyID: 1, Seq: 1}, f.keys.Signing)
	if _, err := f.svc.Ingest(ctx, ack, ChannelHTTP, 0); !errors.Is(err, payload.ErrFormat) {
		t.Fatalf("ack as signed: %v", err)
	}
	alert := f.signed(payload.KindAlert, 0, true)
	for _, h := range []int{256, -2} {
		if _, err := f.svc.Ingest(ctx, alert, ChannelMesh, h); !errors.Is(err, payload.ErrFormat) {
			t.Fatalf("hops %d: %v", h, err)
		}
	}
	_, err := f.svc.Ingest(ctx, alert, Channel("fax"), 0)
	if err == nil || errors.Is(err, payload.ErrFormat) || errors.Is(err, payload.ErrSignature) || errors.Is(err, payload.ErrDecrypt) {
		t.Fatalf("fax: %v", err)
	}
}

func TestCancelClosesAndPublishes(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	if _, err := f.svc.Ingest(ctx, f.signed(payload.KindAlert, 0, true), ChannelHTTP, 0); err != nil {
		t.Fatal(err)
	}
	cancel := f.signed(payload.KindCancel, 1, false)
	res, err := f.svc.Ingest(ctx, cancel, ChannelHTTP, 0)
	if err != nil {
		t.Fatal(err)
	}
	f.mustAck(res, 1)
	evs := f.pub.all()
	if len(evs) != 2 || evs[1].Status != StatusCancelled {
		t.Fatalf("events = %+v", evs)
	}
	if _, err := f.svc.Ingest(ctx, cancel, ChannelHTTP, 0); err != nil {
		t.Fatal(err)
	}
	if len(f.pub.all()) != 2 {
		t.Fatal("cancel published twice")
	}
}

func TestDuplicateDoesNotPublish(t *testing.T) {
	f := newFixture(t)
	alert := f.signed(payload.KindAlert, 0, true)
	for i := 0; i < 3; i++ {
		if _, err := f.svc.Ingest(context.Background(), alert, ChannelHTTP, 0); err != nil {
			t.Fatal(err)
		}
	}
	if n := len(f.pub.all()); n != 1 {
		t.Fatalf("events = %d", n)
	}
}

func TestConflictIsNotAcked(t *testing.T) {
	f := newFixture(t)
	orig := Emergency{ID: svcEmergencyID, DeviceID: "99999999-2222-4333-8444-555555555555", Status: StatusOpen,
		Channels: []Channel{ChannelHTTP}, MinHops: HopsUnknown, FirstReceivedAt: t0, UpdatedAt: t0, SentAt: t0}
	f.store.records[svcEmergencyID] = orig
	res, err := f.svc.Ingest(context.Background(), f.signed(payload.KindCancel, 1, false), ChannelHTTP, 0)
	if !errors.Is(err, ErrConflict) || res.Ack != nil {
		t.Fatalf("res=%+v err=%v", res, err)
	}
	if got := f.store.records[svcEmergencyID]; got.Status != StatusOpen || got.DeviceID != orig.DeviceID {
		t.Fatalf("record changed: %+v", got)
	}
	if len(f.pub.all()) != 0 {
		t.Fatal("published")
	}
}

func TestIngestMeshWrongServerKeyIsDecryptError(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	stale, err := serverkeys.Generate(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	alert := f.signed(payload.KindAlert, 0, true)
	if _, err := f.svc.IngestMesh(ctx, f.sealedTo(stale, alert), 1); !errors.Is(err, payload.ErrDecrypt) {
		t.Fatalf("stale key: %v", err)
	}
	ack := payload.SignAck(payload.Ack{EmergencyID: 1, Seq: 1}, f.keys.Signing)
	if _, err := f.svc.IngestMesh(ctx, payload.MeshAckText(ack), 1); !errors.Is(err, payload.ErrFormat) {
		t.Fatalf("ack text: %v", err)
	}
	if _, err := f.svc.IngestMesh(ctx, payload.SMSText(alert), 1); !errors.Is(err, payload.ErrFormat) {
		t.Fatalf("sms text: %v", err)
	}
}

func TestConcurrentReceiptsMakeOneRecord(t *testing.T) {
	f := newFixture(t)
	alert := f.signed(payload.KindAlert, 0, true)
	text := f.sealed(alert)
	var wg sync.WaitGroup
	acks := make([]Result, 20)
	errs := make([]error, 20)
	for i := 0; i < 20; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if i%2 == 0 {
				acks[i], errs[i] = f.svc.IngestMesh(context.Background(), text, 1)
			} else {
				acks[i], errs[i] = f.svc.Ingest(context.Background(), alert, ChannelHTTP, 0)
			}
		}()
	}
	wg.Wait()
	for i := range acks {
		if errs[i] != nil {
			t.Fatal(errs[i])
		}
		f.mustAck(acks[i], 0)
	}
	if len(f.store.records) != 1 || len(f.pub.all()) != 1 {
		t.Fatalf("records=%d events=%d", len(f.store.records), len(f.pub.all()))
	}
}
