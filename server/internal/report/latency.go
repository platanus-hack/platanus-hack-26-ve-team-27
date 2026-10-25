package report

import (
	"strconv"
	"strings"
	"time"

	"github.com/platanus-hack/platanus-hack-26-ve-team-27/server/internal/contract"
)

// Latency is one of the big numbers at the top of the page: how long after
// the gesture (phone clock) something happened.
type Latency struct {
	Key     string
	Label   string
	Value   string        // "0,4 s"; "" while pending
	D       time.Duration // the measured latency, when Value is set
	Pending bool          // expected and not there yet: the page counts up live
	Note    string
}

// Latencies are measured from the gesture (triggered_at, phone clock), so
// each mixes the phone's clock with the server's: a phone clock ahead of the
// server's shows as a negative number.
//
//   - Gesto → servidor: the alert reached the server (incidents.created_at).
//   - Gesto → Mamá llamando: the first call was requested (call branch only).
//   - Gesto → sonó: Twilio first said ringing, when the engine polled it.
//   - Gesto → WhatsApp: the family message was claimed (notified_at), which
//     is just before sending, not a delivery receipt.
//
// live is whether the case is still open: a latency that has not happened
// is then Pending (the page counts it up), else "—". callActive says a call
// is being placed now.
func Latencies(rec Record, live, callActive bool) []Latency {
	inc := rec.Incident
	since := func(t time.Time) time.Duration { return t.Sub(inc.TriggeredAt) }
	measured := func(l Latency, t time.Time) Latency {
		l.D = since(t)
		l.Value = latencyText(l.D)
		if l.D < 0 {
			l.Note = "el reloj del teléfono va adelantado"
		}
		return l
	}
	missing := func(l Latency, why string) Latency {
		if live {
			l.Pending = true
			return l
		}
		l.Value, l.Note = "—", why
		return l
	}

	out := []Latency{measured(Latency{Key: "server", Label: "Gesto → servidor"}, inc.CreatedAt)}
	if inc.Branch == contract.BranchCall {
		l := Latency{Key: "calling", Label: "Gesto → Mamá llamando"}
		if len(rec.Calls) > 0 {
			out = append(out, measured(l, rec.Calls[0].RequestedAt))
		} else {
			out = append(out, missing(l, "no se registró la llamada"))
		}
		var ring *time.Time
		polled := false
		for _, c := range rec.Calls {
			if c.RingingAt != nil && ring == nil {
				ring = c.RingingAt
			}
			polled = polled || c.TwilioStatus != ""
		}
		l = Latency{Key: "ringing", Label: "Gesto → sonó", Note: "según Twilio"}
		switch {
		case ring != nil:
			l = measured(l, *ring)
			if l.Note == "" {
				l.Note = "según Twilio"
			}
			out = append(out, l)
		case callActive && polled:
			l.Pending = true
			out = append(out, l)
		}
	}
	l := Latency{Key: "whatsapp", Label: "Gesto → WhatsApp"}
	if inc.NotifiedAt != nil {
		out = append(out, measured(l, *inc.NotifiedAt))
	} else {
		out = append(out, missing(l, "sin aviso registrado"))
	}
	return out
}

// latencyText is a latency in big-number form: "0,4 s", "12,3 s",
// "1 min 8 s", "-0,3 s".
func latencyText(d time.Duration) string {
	sign := ""
	if d < 0 {
		sign, d = "−", -d
	}
	if d < time.Minute {
		return sign + decimal(d.Seconds(), 1) + " s"
	}
	return sign + duration(d.Truncate(time.Second))
}

// channelsLabel names the offline emergency channels: "SMS y malla".
func channelsLabel(channels []string) string {
	names := make([]string, 0, len(channels))
	for _, c := range channels {
		switch c {
		case "http":
			names = append(names, "internet")
		case "sms":
			names = append(names, "SMS")
		case "mesh":
			names = append(names, "malla")
		default:
			names = append(names, c)
		}
	}
	switch len(names) {
	case 0:
		return "—"
	case 1:
		return names[0]
	}
	return strings.Join(names[:len(names)-1], ", ") + " y " + names[len(names)-1]
}

// hopsLabel is " (2 saltos)" for a mesh relay, "" when unknown.
func hopsLabel(hops *int) string {
	switch {
	case hops == nil:
		return ""
	case *hops == 1:
		return " (1 salto)"
	}
	return " (" + strconv.Itoa(*hops) + " saltos)"
}

// EmergencyView is an offline emergency in the page.
type EmergencyView struct {
	Key      string
	ID       string
	Status   string
	Channels string
	Received string // server clock
	Sent     string // phone clock
	LatLng   string
	MapsURL  string
}

func emergencyViews(ems []Emergency) []EmergencyView {
	var out []EmergencyView
	for _, e := range ems {
		v := EmergencyView{
			Key: "em-" + e.ID, ID: e.ID, Channels: channelsLabel(e.Channels) + hopsLabel(e.MinHops),
			Received: clock(e.FirstReceivedAt), Sent: clock(e.SentAt),
		}
		switch e.Status {
		case "open":
			v.Status = "abierta"
		case "acked":
			v.Status = "confirmada"
		case "cancelled":
			v.Status = "cancelada"
		default:
			v.Status = e.Status
		}
		if e.Lat != nil && e.Lng != nil {
			v.LatLng = strconv.FormatFloat(*e.Lat, 'f', 5, 64) + ", " + strconv.FormatFloat(*e.Lng, 'f', 5, 64)
			v.MapsURL = MapsURL(*e.Lat, *e.Lng)
		}
		out = append(out, v)
	}
	return out
}
