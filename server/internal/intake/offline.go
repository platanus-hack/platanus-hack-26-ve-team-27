package intake

import (
	"context"
	"crypto/sha256"
	"fmt"
	"log"
	"time"

	"github.com/platanus-hack/platanus-hack-26-ve-team-27/server/internal/contract"
	"github.com/platanus-hack/platanus-hack-26-ve-team-27/server/internal/emergency/ingest"
)

// OfflinePublisher turns offline emergencies (#57) into incidents. It is the
// ingest.Publisher, so every channel lands here: POST /emergencies, the
// inbound SMS webhook (#58) and the BLE relay's POST /emergencies/relay (#60).
//
//   - The first open receipt creates an incident with trigger "offline" and
//     branch immediate_alert (a phone without signal cannot take Mamá's call),
//     stores the fix as a GPS point and hands the incident to the notifier.
//   - A later frame with a new fix adds a GPS point to the same incident.
//   - An emergency first seen cancelled opens nothing. A cancel after the
//     incident only stays on the emergency record (the report shows it).
//
// The incident id is derived from the emergency id, so the same emergency
// arriving over several channels, or an event published twice, is one
// incident, and the notifier's claim sends one family message. Never logs the
// location.
type OfflinePublisher struct{ h *Handler }

var _ ingest.Publisher = OfflinePublisher{}

// OfflinePublisher returns the publisher over this handler's pool, call
// engine and notifier.
func (h *Handler) OfflinePublisher() OfflinePublisher { return OfflinePublisher{h: h} }

// Publish stores the event as an incident and its point. Called from the
// ingest service's goroutine after the emergency is committed.
func (p OfflinePublisher) Publish(ctx context.Context, ev ingest.EmergencyReceived) {
	if p.h.pool == nil {
		return
	}
	if ev.Status == ingest.StatusCancelled || p.cancelled(ctx, ev.EmergencyID) {
		// Events may arrive out of order: a stored cancel is terminal.
		log.Printf("intake: offline emergency %s cancelled; no incident", ev.EmergencyID)
		return
	}
	alert := offlineAlert(ev)
	resp, created, err := createIncident(ctx, p.h.pool, alert, contract.BranchImmediateAlert, nil)
	if err != nil {
		log.Printf("intake: offline emergency %s: create incident: %v", ev.EmergencyID, err)
		return
	}
	log.Printf("intake: offline emergency %s via %s hops %d -> incident %s (new %t, fix %t)",
		ev.EmergencyID, ev.Channel, ev.Hops, alert.IncidentID, created, len(alert.Points) > 0)
	if !created {
		return
	}
	inc := Incident{
		ID:          alert.IncidentID,
		DeviceID:    alert.DeviceID,
		Branch:      resp.Branch,
		TriggeredAt: alert.TriggeredAt,
	}
	if last := alert.LastPoint(); last != nil {
		inc.Location = *last
	}
	p.h.handoff(ctx, inc)
}

// cancelled reports whether the stored emergency is already cancelled. A
// failed read counts as not cancelled: the alert matters more.
func (p OfflinePublisher) cancelled(ctx context.Context, emergencyID string) bool {
	var status string
	err := p.h.pool.QueryRowContext(ctx, `SELECT status FROM emergencies WHERE emergency_id = $1`, emergencyID).Scan(&status)
	return err == nil && status == string(ingest.StatusCancelled)
}

// offlineAlert is the AlertEvent an offline emergency stands for: its fix,
// if any, recorded at the payload time minus the fix's age (phone clock).
func offlineAlert(ev ingest.EmergencyReceived) contract.AlertEvent {
	alert := contract.AlertEvent{
		IncidentID:  OfflineIncidentID(ev.EmergencyID),
		DeviceID:    ev.DeviceID,
		Trigger:     contract.TriggerOffline,
		TriggeredAt: ev.SentAt,
	}
	if loc := ev.Location; loc != nil {
		alert.Points = []contract.Point{{
			Lat:        loc.Lat(),
			Lng:        loc.Lon(),
			RecordedAt: loc.SentAt.Add(-time.Duration(loc.AgeS) * time.Second),
		}}
	}
	return alert
}

// OfflineIncidentID is the incident id of an offline emergency: a UUID (version
// 8, RFC 9562) over SHA-256 of the emergency id, the same for every receipt.
func OfflineIncidentID(emergencyID string) string {
	sum := sha256.Sum256([]byte("mama-offline-incident:" + emergencyID))
	b := sum[:16]
	b[6] = b[6]&0x0f | 0x80
	b[8] = b[8]&0x3f | 0x80
	return fmt.Sprintf("%x-%x-%x-%x-%x", b[0:4], b[4:6], b[6:8], b[8:10], b[10:16])
}
