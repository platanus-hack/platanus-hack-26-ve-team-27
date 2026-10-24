// Package intake takes the phone's messages (docs/contract.md): device
// registrations, alert events and location updates. It stores the incident
// and its GPS points, picks the branch from the speed and hands a new
// incident to the call engine or the notifier.
//
// Status codes:
//
//	POST /devices                          204 stored; 400 bad body, malformed ed25519_public_key,
//	                                       or missing/invalid ed25519_signature; 409
//	                                       ed25519_public_key registered to another device
//	POST /incidents                        201 new incident, 200 retried incident_id (stored decision);
//	                                       400 bad body; 409 incident_id taken by another device;
//	                                       422 device not registered
//	POST /incidents/{incident_id}/points   204 stored (resent fixes ignored); 400 bad body or
//	                                       path id != body incident_id; 403 incident belongs to
//	                                       another device; 404 unknown incident
//
// Every route answers 503 when the server has no database, and 500 with no
// detail when a query fails. Errors are {"error": "..."}.
package intake

import (
	"context"
	"crypto/ed25519"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net/http"
	"regexp"

	"github.com/platanus-hack/platanus-hack-26-ve-team-27/server/internal/contract"
)

// maxBodyBytes bounds a request body; an alert carries a handful of fixes.
const maxBodyBytes = 1 << 20

// Handler serves the intake routes. Create it with New.
type Handler struct {
	pool     *sql.DB
	calls    CallStarter
	notifier Notifier
	// spawn runs a handoff without blocking the response. Tests replace it.
	spawn func(func())
}

// New returns the intake handler. pool may be nil (no DATABASE_URL): every
// route then answers 503.
func New(pool *sql.DB, calls CallStarter, notifier Notifier) *Handler {
	return &Handler{pool: pool, calls: calls, notifier: notifier, spawn: func(f func()) { go f() }}
}

// Register mounts the intake routes on mux.
func (h *Handler) Register(mux *http.ServeMux) {
	mux.HandleFunc("POST /devices", h.handleDevice)
	mux.HandleFunc("POST /incidents", h.handleAlert)
	mux.HandleFunc("POST /incidents/{incident_id}/points", h.handlePoints)
}

func (h *Handler) handleDevice(w http.ResponseWriter, r *http.Request) {
	if !h.ready(w) {
		return
	}
	var reg contract.DeviceRegistration
	if !decode(w, r, &reg) {
		return
	}
	if err := validateDevice(reg); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	switch err := upsertDevice(r.Context(), h.pool, reg); {
	case errors.Is(err, errKeyTaken):
		writeError(w, http.StatusConflict, "ed25519_public_key is registered to another device")
		return
	case err != nil:
		internalError(w, "upsert device", err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (h *Handler) handleAlert(w http.ResponseWriter, r *http.Request) {
	if !h.ready(w) {
		return
	}
	var ev contract.AlertEvent
	if !decode(w, r, &ev) {
		return
	}
	if err := validateAlert(ev); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}

	branch := ev.Branch()
	speed := branchSpeed(ev.LastPoint())
	resp, created, err := createIncident(r.Context(), h.pool, ev, branch, speed)
	switch {
	case errors.Is(err, errUnknownDevice):
		writeError(w, http.StatusUnprocessableEntity, "device not registered; send POST /devices first")
		return
	case errors.Is(err, errDeviceMismatch):
		writeError(w, http.StatusConflict, "incident_id belongs to another device")
		return
	case err != nil:
		internalError(w, "create incident", err)
		return
	}

	status := http.StatusOK
	if created {
		status = http.StatusCreated
		h.handoff(r.Context(), Incident{
			ID:          ev.IncidentID,
			DeviceID:    ev.DeviceID,
			Branch:      branch,
			SpeedMPS:    speed,
			Location:    *ev.LastPoint(),
			TriggeredAt: ev.TriggeredAt,
		})
	}
	writeJSON(w, status, resp)
}

func (h *Handler) handlePoints(w http.ResponseWriter, r *http.Request) {
	if !h.ready(w) {
		return
	}
	var up contract.LocationUpdate
	if !decode(w, r, &up) {
		return
	}
	if up.IncidentID != r.PathValue("incident_id") {
		writeError(w, http.StatusBadRequest, "incident_id in the body does not match the path")
		return
	}
	if err := validateUpdate(up); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	_, err := addPoints(r.Context(), h.pool, up.IncidentID, up.DeviceID, up.Points)
	switch {
	case errors.Is(err, errUnknownIncident):
		writeError(w, http.StatusNotFound, "incident not found")
		return
	case errors.Is(err, errDeviceMismatch):
		writeError(w, http.StatusForbidden, "incident belongs to another device")
		return
	case err != nil:
		internalError(w, "add points", err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// handoff gives a new incident to the call engine (BranchCall) or the
// notifier (the other two) without blocking the response.
func (h *Handler) handoff(ctx context.Context, inc Incident) {
	ctx = context.WithoutCancel(ctx)
	h.spawn(func() {
		defer func() {
			if p := recover(); p != nil {
				log.Printf("intake: handoff of incident %s panicked: %v", inc.ID, p)
			}
		}()
		if inc.Branch == contract.BranchCall {
			h.calls.StartCall(ctx, inc)
		} else {
			h.notifier.Notify(ctx, inc)
		}
	})
}

func (h *Handler) ready(w http.ResponseWriter) bool {
	if h.pool == nil {
		writeError(w, http.StatusServiceUnavailable, "database not configured")
		return false
	}
	return true
}

// branchSpeed is the speed the branch is picked from, nil when unknown
// (missing or negative), as AlertResponse.speed_mps reports it.
func branchSpeed(p *contract.Point) *float64 {
	if p == nil || p.SpeedMPS == nil || !(*p.SpeedMPS >= 0) {
		return nil
	}
	s := *p.SpeedMPS
	return &s
}

// decode parses the body strictly, like the contract round-trip tests: an
// unknown field or trailing data is a 400.
func decode(w http.ResponseWriter, r *http.Request, v any) bool {
	dec := json.NewDecoder(http.MaxBytesReader(w, r.Body, maxBodyBytes))
	dec.DisallowUnknownFields()
	err := dec.Decode(v)
	if err == nil {
		if _, extra := dec.Token(); extra != io.EOF {
			err = errors.New("trailing data after the JSON object")
		}
	}
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid JSON: "+err.Error())
		return false
	}
	return true
}

var uuidRE = regexp.MustCompile(`^[0-9a-fA-F]{8}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{12}$`)

func validateDevice(reg contract.DeviceRegistration) error {
	switch {
	case reg.DeviceID == "":
		return errors.New("device_id is required")
	case reg.PhoneNumber == "":
		return errors.New("phone_number is required")
	case len(reg.Ed25519PublicKey) != 0 && len(reg.Ed25519PublicKey) != ed25519.PublicKeySize:
		return errors.New("ed25519_public_key must be 32 bytes of standard Base64")
	}
	return validateKeyProof(reg)
}

// validateKeyProof checks that a registration carrying a key also proves it
// holds the private half (docs/contract.md): bitchat broadcasts every
// device's public key on the mesh, so without the signature anyone could
// register a victim's key to their own device_id first.
func validateKeyProof(reg contract.DeviceRegistration) error {
	switch {
	case len(reg.Ed25519PublicKey) == 0 && len(reg.Ed25519Signature) != 0:
		return errors.New("ed25519_signature without ed25519_public_key")
	case len(reg.Ed25519PublicKey) == 0:
		return nil
	case len(reg.Ed25519Signature) == 0:
		return errors.New("ed25519_signature is required with ed25519_public_key")
	case len(reg.Ed25519Signature) != ed25519.SignatureSize:
		return errors.New("ed25519_signature must be 64 bytes of standard Base64")
	case !ed25519.Verify(ed25519.PublicKey(reg.Ed25519PublicKey), contract.RegistrationSignedMessage(reg.DeviceID), reg.Ed25519Signature):
		return errors.New("ed25519_signature does not verify for this device_id")
	}
	return nil
}

func validateAlert(ev contract.AlertEvent) error {
	switch {
	case !uuidRE.MatchString(ev.IncidentID):
		return errors.New("incident_id must be a UUID")
	case ev.DeviceID == "":
		return errors.New("device_id is required")
	case !validTrigger(ev.Trigger):
		return fmt.Errorf("trigger %q is not one of volume_pattern, widget, tile, in_app", ev.Trigger)
	case ev.TriggeredAt.IsZero():
		return errors.New("triggered_at is required")
	}
	return validatePoints(ev.Points)
}

func validateUpdate(up contract.LocationUpdate) error {
	switch {
	case !uuidRE.MatchString(up.IncidentID):
		return errors.New("incident_id must be a UUID")
	case up.DeviceID == "":
		return errors.New("device_id is required")
	}
	return validatePoints(up.Points)
}

func validatePoints(points []contract.Point) error {
	if len(points) == 0 {
		return errors.New("points needs at least one fix")
	}
	for i, p := range points {
		switch {
		case p.Lat < -90 || p.Lat > 90 || p.Lng < -180 || p.Lng > 180:
			return fmt.Errorf("points[%d]: lat/lng out of range", i)
		case p.RecordedAt.IsZero():
			return fmt.Errorf("points[%d]: recorded_at is required", i)
		}
	}
	return nil
}

func validTrigger(t contract.Trigger) bool {
	switch t {
	case contract.TriggerVolumePattern, contract.TriggerWidget, contract.TriggerTile, contract.TriggerInApp:
		return true
	}
	return false
}

func writeJSON(w http.ResponseWriter, status int, body any) {
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(body)
}

func writeError(w http.ResponseWriter, status int, msg string) {
	writeJSON(w, status, map[string]string{"error": msg})
}

// internalError logs err and answers 500 without leaking it.
func internalError(w http.ResponseWriter, what string, err error) {
	log.Printf("intake: %s: %v", what, err)
	writeError(w, http.StatusInternalServerError, "internal error")
}
