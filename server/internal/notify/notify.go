// Package notify sends the family the one message of an incident
// (docs/contract.md "NotifierMessage"): the location and the verdict. Its
// only link is the private incident report (#76), added when the report is
// on (WithReportLinks). The incident is claimed in the database before
// sending, so a retried or concurrent Notify sends at most once per incident.
//
// The channel is a Sender: Kapso WhatsApp (the default) or Twilio SMS,
// picked by the NOTIFIER env var (see NewSender).
package notify

import (
	"context"
	"fmt"
	"log"
	"math"
	"strings"
	"time"

	"github.com/platanus-hack/platanus-hack-26-ve-team-27/server/internal/contract"
	"github.com/platanus-hack/platanus-hack-26-ve-team-27/server/internal/intake"
)

// Sender delivers one text message to one E.164 number.
type Sender interface {
	Send(ctx context.Context, to, body string) error
}

// Store reads what the message needs and owns the at-most-once claim.
type Store interface {
	// Message builds the NotifierMessage of inc: family numbers, newest
	// location and the incident verdict. It has no side effects.
	Message(ctx context.Context, inc intake.Incident) (contract.NotifierMessage, error)
	// Claim stamps notified_at if it is still NULL and reports whether this
	// caller won the incident.
	Claim(ctx context.Context, incidentID string) (bool, error)
	// Release clears notified_at so a later Notify can send again.
	Release(ctx context.Context, incidentID string) error
}

// ReportLinks gives the private incident report link (#76) for the message.
// report.Links is the real one.
type ReportLinks interface {
	// ReportURL returns the report's absolute URL, creating its link when
	// needed; "" when there is none to send.
	ReportURL(ctx context.Context, incidentID string) (string, error)
}

// IDSender is a Sender that also returns the provider's message id
// (*Kapso: the WhatsApp wamid).
type IDSender interface {
	SendWithID(ctx context.Context, to, body string) (string, error)
}

// DeliveryLog records the outcome of each send, so the incident report
// (#76) can show that every relative was notified. report.DeliveryLog is
// the real one. channel is "whatsapp" or "sms" (ChannelOf the sender that
// sent it); messageID is "" when the send failed or the provider gave none.
type DeliveryLog interface {
	Sent(ctx context.Context, incidentID, to, channel, messageID string, err error)
}

// ChannelOf names a Sender's channel for the delivery log: "whatsapp" for
// Kapso, "sms" for Twilio, "" for anything else.
func ChannelOf(s Sender) string {
	switch s.(type) {
	case *Kapso:
		return "whatsapp"
	case *SMS:
		return "sms"
	}
	return ""
}

// Notifier implements intake.Notifier. Create it with New.
type Notifier struct {
	store      Store
	sender     Sender
	links      ReportLinks // nil: no report line
	deliveries DeliveryLog // nil: sends are only logged
}

var _ intake.Notifier = (*Notifier)(nil)

// New returns a Notifier that claims through store and sends through sender.
func New(store Store, sender Sender) *Notifier {
	return &Notifier{store: store, sender: sender}
}

// WithDeliveries records every send's outcome in log.
func (n *Notifier) WithDeliveries(log DeliveryLog) *Notifier {
	n.deliveries = log
	return n
}

// send sends one message, with the provider's message id when it gives one,
// and records the outcome.
func (n *Notifier) send(ctx context.Context, incidentID, to, body string) error {
	var id string
	var err error
	if s, ok := n.sender.(IDSender); ok {
		id, err = s.SendWithID(ctx, to, body)
	} else {
		err = n.sender.Send(ctx, to, body)
	}
	if n.deliveries != nil {
		n.deliveries.Sent(context.WithoutCancel(ctx), incidentID, to, ChannelOf(n.sender), id, err)
	}
	return err
}

// WithReportLinks adds the "Ficha: <url>" line to every message, from links.
func (n *Notifier) WithReportLinks(links ReportLinks) *Notifier {
	n.links = links
	return n
}

// Notify sends the incident's message to every family number, once. It only
// logs failures: the handoff has no caller to report to.
//
// If no family number got the message, the claim is released so a retry can
// send. Once any number got it, the claim stays, so a retry never sends a
// second message.
func (n *Notifier) Notify(ctx context.Context, inc intake.Incident) {
	msg, err := n.store.Message(ctx, inc)
	if err != nil {
		log.Printf("notify: incident %s: load message: %v", inc.ID, err)
		return
	}
	if len(msg.To) == 0 {
		log.Printf("notify: incident %s: the device has no family numbers; nothing sent", inc.ID)
		return
	}

	won, err := n.store.Claim(ctx, inc.ID)
	if err != nil {
		log.Printf("notify: incident %s: claim: %v", inc.ID, err)
		return
	}
	if !won {
		log.Printf("notify: incident %s: already notified; skipping", inc.ID)
		return
	}

	body := FormatWithReport(msg, n.reportURL(ctx, inc.ID))
	sent := 0
	for _, to := range msg.To {
		if err := n.send(ctx, inc.ID, to, body); err != nil {
			log.Printf("notify: incident %s: send to %s: %v", inc.ID, mask(to), err)
			continue
		}
		sent++
	}
	if sent > 0 {
		log.Printf("notify: incident %s: sent to %d of %d family numbers", inc.ID, sent, len(msg.To))
		return
	}
	// Nobody got it: release so a retry can try again.
	if err := n.store.Release(context.WithoutCancel(ctx), inc.ID); err != nil {
		log.Printf("notify: incident %s: release claim after failed send: %v", inc.ID, err)
	}
}

// reportURL is the report link for the message, "" without one. A failure
// only drops the line: the alert matters more than the link.
func (n *Notifier) reportURL(ctx context.Context, incidentID string) string {
	if n.links == nil {
		return ""
	}
	u, err := n.links.ReportURL(ctx, incidentID)
	if err != nil {
		log.Printf("notify: incident %s: report link: %v; sending without it", incidentID, err)
		return ""
	}
	return u
}

// vet is Venezuela time (UTC-4, no DST). A fixed zone needs no tzdata on the
// server image.
var vet = time.FixedZone("VET", -4*60*60)

// Format renders the family message in Spanish: who, where, the verdict and
// the time, with no link. FormatWithReport adds the report's.
//
//	🚨 Alerta de MAMÁ
//	Ubicación: 10.4961, -66.8531 (±12 m)
//	Veredicto: coacción
//	Hora: 09:10 VET
func Format(msg contract.NotifierMessage) string { return FormatWithReport(msg, "") }

// FormatWithReport is Format plus, when reportURL is not blank, a last line
// with the private incident report (#76), the message's only link. Kapso
// sends it with preview_url false, so WhatsApp fetches nothing from it.
//
//	Ficha: https://mama.example.com/i/Mq-7vD8oe12cyws_nqXsWg
func FormatWithReport(msg contract.NotifierMessage, reportURL string) string {
	var b strings.Builder
	b.WriteString("🚨 Alerta de MAMÁ\n")
	fmt.Fprintf(&b, "Ubicación: %.4f, %.4f", msg.Location.Lat, msg.Location.Lng)
	if a := msg.Location.AccuracyM; a != nil && *a >= 0 && !math.IsInf(*a, 0) {
		fmt.Fprintf(&b, " (±%.0f m)", *a)
	}
	b.WriteString("\n")
	fmt.Fprintf(&b, "Veredicto: %s\n", verdictLabel(msg))
	fmt.Fprintf(&b, "Hora: %s VET", msg.TriggeredAt.In(vet).Format("15:04"))
	if reportURL != "" {
		fmt.Fprintf(&b, "\nFicha: %s", reportURL)
	}
	return b.String()
}

// verdictLabel is the Spanish verdict line. With no call (immediate_alert,
// silent_tracker) it says why there was no call instead.
func verdictLabel(msg contract.NotifierMessage) string {
	switch msg.Verdict {
	case contract.VerdictDuress:
		return "coacción"
	case contract.VerdictNoDuress:
		return "sin coacción"
	case contract.VerdictUnclear:
		return "no está claro"
	}
	switch msg.Branch {
	case contract.BranchImmediateAlert:
		return "sin llamada, alerta inmediata"
	case contract.BranchSilentTracker:
		return "sin llamada, en movimiento rápido (rastreo silencioso)"
	}
	return "sin veredicto"
}

// mask keeps the last 4 digits of a phone number for the logs.
func mask(phone string) string {
	if len(phone) <= 4 {
		return "****"
	}
	return "****" + phone[len(phone)-4:]
}
