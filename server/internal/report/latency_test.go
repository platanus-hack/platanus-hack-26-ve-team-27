package report

import (
	"strings"
	"testing"
	"time"

	"github.com/platanus-hack/platanus-hack-26-ve-team-27/server/internal/contract"
)

func latencyMap(ls []Latency) map[string]Latency {
	m := map[string]Latency{}
	for _, l := range ls {
		m[l.Key] = l
	}
	return m
}

// The fixture: gesture 13:10:00.253 (phone), server 13:10:00.653, call
// requested 13:10:01.200, family message 13:11:08.000.
func TestLatenciesFromTheGesture(t *testing.T) {
	ls := Latencies(sealedRecord(), false, false)
	got := latencyMap(ls)
	want := map[string]struct {
		value string
		d     time.Duration
	}{
		"server":   {"0,4 s", 400 * time.Millisecond},
		"calling":  {"0,9 s", 947 * time.Millisecond},
		"whatsapp": {"1 min 7 s", 67747 * time.Millisecond},
	}
	if len(ls) != len(want) || ls[0].Key != "server" || ls[len(ls)-1].Key != "whatsapp" {
		t.Fatalf("latencies = %+v", ls)
	}
	for k, w := range want {
		if g := got[k]; g.Value != w.value || g.D != w.d || g.Pending {
			t.Errorf("%s = %q (%s, pending %t), want %q (%s)", k, g.Value, g.D, g.Pending, w.value, w.d)
		}
	}
	if _, ok := got["ringing"]; ok {
		t.Error("ringing shown without Twilio data")
	}
}

func TestRingingLatencyFromTwilio(t *testing.T) {
	rec := openRecord()
	rec.Calls[0].TwilioStatus = "in-progress"
	rec.Calls[0].RingingAt = ptr(at("2026-10-24T13:10:04.500Z"))
	if l := latencyMap(Latencies(rec, true, false))["ringing"]; l.Value != "4,2 s" || l.Note != "según Twilio" || l.Label != "Gesto → sonó" {
		t.Errorf("ringing = %+v", l)
	}
	// Twilio is being polled, the phone has not rung yet: count it live.
	rec.Calls[0].RingingAt = nil
	rec.Calls[0].TwilioStatus = "queued"
	if l := latencyMap(Latencies(rec, true, true))["ringing"]; !l.Pending || l.Value != "" {
		t.Errorf("ringing while dialing = %+v, want pending", l)
	}
}

// Before the call and the family message: live counts them up; a closed
// case says they did not happen.
func TestPendingLatencies(t *testing.T) {
	rec := openRecord()
	rec.Calls, rec.Incident.NotifiedAt = nil, nil
	live := latencyMap(Latencies(rec, true, false))
	if !live["calling"].Pending || !live["whatsapp"].Pending || live["server"].Pending {
		t.Errorf("live = %+v", live)
	}
	closed := latencyMap(Latencies(rec, false, false))
	if c := closed["calling"]; c.Pending || c.Value != "—" || c.Note == "" {
		t.Errorf("closed calling = %+v", c)
	}
	if w := closed["whatsapp"]; w.Value != "—" || w.Note != "sin aviso registrado" {
		t.Errorf("closed whatsapp = %+v", w)
	}
}

func TestLatencyWithThePhoneClockAhead(t *testing.T) {
	rec := openRecord()
	rec.Incident.TriggeredAt = rec.Incident.CreatedAt.Add(2 * time.Second)
	if l := Latencies(rec, false, false)[0]; l.Value != "−2,0 s" || l.D != -2*time.Second || !strings.Contains(l.Note, "adelantado") {
		t.Errorf("server = %+v", l)
	}
}

func TestNoCallBranchesHaveNoCallLatencies(t *testing.T) {
	rec := openRecord()
	rec.Incident.Branch, rec.Calls = contract.BranchImmediateAlert, nil
	ls := Latencies(rec, true, false)
	if len(ls) != 2 || ls[0].Key != "server" || ls[1].Key != "whatsapp" || ls[1].Value != "1 min 7 s" {
		t.Errorf("immediate_alert = %+v", ls)
	}
}

func TestLatencyText(t *testing.T) {
	for d, want := range map[time.Duration]string{
		0:                        "0,0 s",
		400 * time.Millisecond:   "0,4 s",
		12345 * time.Millisecond: "12,3 s",
		59900 * time.Millisecond: "59,9 s",
		67747 * time.Millisecond: "1 min 7 s",
		-300 * time.Millisecond:  "−0,3 s",
		2 * time.Minute:          "2 min",
	} {
		if got := latencyText(d); got != want {
			t.Errorf("latencyText(%s) = %q, want %q", d, got, want)
		}
	}
}

// What the live page needs beyond the #76 table: ringing, a retry, the
// fallback, an offline emergency, and the live call state.
func TestTimelineShowsRingingRetryAndFallback(t *testing.T) {
	rec := openRecord()
	rec.Calls[0].TwilioStatus, rec.Calls[0].RingingAt = "completed", ptr(at("2026-10-24T13:10:04Z"))
	var kinds []string
	for _, e := range Timeline(rec, Options{}) {
		if e.Kind == EventRinging {
			kinds = append(kinds, e.Key+"@"+e.Time()+"/"+string(e.Clock))
		}
	}
	if strings.Join(kinds, ",") != "call-1-ringing@09:10:04/server" {
		t.Errorf("ringing events = %v", kinds)
	}

	failed := func(id int64, req string) Call {
		return Call{ID: id, Status: "failed", RequestedAt: at(req), EndedAt: ptr(at(req).Add(20 * time.Second))}
	}
	rec.Calls = []Call{failed(1, "2026-10-24T13:10:01Z"), failed(2, "2026-10-24T13:10:22Z")}
	ev := Timeline(rec, Options{})
	var retry, fallback *Event
	for i := range ev {
		switch ev[i].Key {
		case "call-2-requested":
			retry = &ev[i]
		case "fallback":
			fallback = &ev[i]
		}
	}
	if retry == nil || retry.Title != "Mamá volvió a llamar" || !strings.HasPrefix(retry.Detail, "Reintento") {
		t.Errorf("retry = %+v", retry)
	}
	if fallback == nil || !fallback.Error || fallback.Time() != "09:10:42" || !strings.Contains(fallback.Detail, "Deducido") {
		t.Errorf("fallback = %+v", fallback)
	}
	if !Build(rec, Options{}).Fallback {
		t.Error("Build does not flag the fallback")
	}
	rec.Calls = rec.Calls[:1] // one failed +58 call: the engine retries, no fallback yet
	if Build(rec, Options{}).Fallback {
		t.Error("fallback after one failed +58 call")
	}
}

func TestEveryEventHasAUniqueKey(t *testing.T) {
	rec := sealedRecord()
	rec.Calls[0].RingingAt = ptr(at("2026-10-24T13:10:03Z"))
	rec.Emergencies = []Emergency{{ID: "00000000000000aa", Status: "cancelled", Channels: []string{"sms", "mesh"}, MinHops: ptr(2),
		FirstReceivedAt: at("2026-10-24T13:09:50Z"), UpdatedAt: at("2026-10-24T13:12:00Z"), CancelledAt: ptr(at("2026-10-24T13:12:00Z"))}}
	seen := map[string]bool{}
	for _, e := range Timeline(rec, Options{}) {
		if e.Key == "" || seen[e.Key] {
			t.Errorf("key %q empty or repeated", e.Key)
		}
		seen[e.Key] = true
		if e.Key == "emergency-00000000000000aa" && e.Detail != "Llegó por SMS y malla (2 saltos) · mismo teléfono, misma hora" {
			t.Errorf("emergency detail = %q", e.Detail)
		}
	}
	if !seen["emergency-00000000000000aa-cancelled"] {
		t.Error("no cancellation event")
	}
}

// While the call is placed the page shows it live, with Mamá calling; a
// call left "in progress" for ages is not live any more.
func TestLiveCallState(t *testing.T) {
	rec := openRecord()
	c := &rec.Calls[0]
	c.Status, c.EndedAt, c.DurationSecs, c.Transcript, c.Verdict, c.VerdictAt = "in_progress", nil, nil, nil, "", nil
	c.RingingAt = ptr(at("2026-10-24T13:10:04Z"))
	now := at("2026-10-24T13:10:20Z")
	p := Build(rec, Options{CanSeal: true, Now: now})
	if !p.Live || !p.CallActive || p.CallPhase != "Sonando" || p.CallSince != "2026-10-24T13:10:04Z" || p.Character != "llamando" {
		t.Errorf("ringing: live %t active %t phase %q since %q art %q", p.Live, p.CallActive, p.CallPhase, p.CallSince, p.Character)
	}
	c.PickedUpAt = ptr(at("2026-10-24T13:10:07Z"))
	if p := Build(rec, Options{CanSeal: true, Now: now}); p.CallPhase != "En llamada" || !p.Calls[0].Active || p.Calls[0].PickedUp != "09:10:07" {
		t.Errorf("answered: %q %+v", p.CallPhase, p.Calls[0])
	}
	stale := Build(rec, Options{CanSeal: true, Now: at("2026-10-24T14:30:00Z")})
	if stale.CallActive || stale.Calls[0].Active || stale.Live || stale.Character != "escuchando" {
		t.Errorf("an hour later: active %t live %t art %q", stale.CallActive, stale.Live, stale.Character)
	}
	if sealed := Build(sealedRecord(), Options{Now: now}); sealed.Live || sealed.CallActive {
		t.Error("a sealed case is live")
	}
}
