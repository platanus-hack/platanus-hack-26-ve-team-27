// Package intake takes the phone's messages (docs/contract.md): device
// registrations, alert events and location updates. It stores the incident
// and its GPS points, picks the branch from the speed and hands a new
// incident to the call engine or the notifier.
//
// Status codes:
//
//	POST /devices                          204 stored; 400 bad body, malformed ed25519_public_key,
//	                                       or missing/invalid ed25519_signature; 409
//	                                       ed25519_public_key registered to another device; 422
//	                                       user_name not 1 to 30 characters, duress_phrase or
//	                                       safe_phrase over 150 characters or 20 words, or
//	                                       safe_phrase without a word or too close to duress_phrase
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
	"strings"
	"sync"
	"unicode"
	"unicode/utf8"

	"github.com/platanus-hack/platanus-hack-26-ve-team-27/server/internal/contract"
	"github.com/platanus-hack/platanus-hack-26-ve-team-27/server/internal/duress"
	"github.com/platanus-hack/platanus-hack-26-ve-team-27/server/internal/trace"
)

// maxBodyBytes bounds a request body. The app's alert, with its one fix, is
// under 400 bytes and a registration under 2 KiB; 16 KiB still holds
// maxPoints fixes indented. At 1 MiB one body held some 17,000 short fixes.
const maxBodyBytes = 16 << 10

// maxPoints bounds the fixes of one request. The app sends one per request:
// the first fix with the alert, then one LocationUpdate at most every 10 s
// for 2 minutes. 20 is ten times the contract's two-fix sample and more than
// all 13 fixes of an incident at once. Each fix is a gps_points row, and
// there is no auth: without the cap a few hundred requests filled the disk.
const maxPoints = 20

// Handler serves the intake routes. Create it with New.
type Handler struct {
	pool     *sql.DB
	calls    CallStarter
	notifier Notifier
	// spawn runs a handoff without blocking the response. Tests replace it.
	spawn func(func())
}

// handoffs counts the handoffs still running, across handlers, so main can
// let them finish when Render stops the instance (WaitHandoffs).
var handoffs sync.WaitGroup

// New returns the intake handler. pool may be nil (no DATABASE_URL): every
// route then answers 503.
func New(pool *sql.DB, calls CallStarter, notifier Notifier) *Handler {
	return &Handler{pool: pool, calls: calls, notifier: notifier, spawn: func(f func()) {
		handoffs.Add(1)
		go func() {
			defer handoffs.Done()
			f()
		}()
	}}
}

// WaitHandoffs blocks until every running handoff has returned, or ctx ends.
// On a deploy, a call being placed or an alert being sent then completes
// instead of dying with the old instance: the phone's retry of that alert gets
// 200 from the new one and would not hand it off again.
func WaitHandoffs(ctx context.Context) error {
	done := make(chan struct{})
	go func() {
		handoffs.Wait()
		close(done)
	}()
	select {
	case <-done:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
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
	reg.UserName, reg.SafePhrase = strings.TrimSpace(reg.UserName), strings.TrimSpace(reg.SafePhrase)
	if err := validateSetup(reg); err != nil {
		writeError(w, http.StatusUnprocessableEntity, err.Error())
		return
	}
	switch err := upsertDevice(r.Context(), h.pool, reg); {
	case errors.Is(err, errKeyTaken):
		writeError(w, http.StatusConflict, "ed25519_public_key is registered to another device")
		return
	case errors.Is(err, errKeyRequired):
		writeError(w, http.StatusConflict, "device has a registered key; registration must carry it")
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

	branch := chooseBranch(ev)
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

	trace.Log(ev.IncidentID, "alert_received", "trigger", string(ev.Trigger), "branch", string(branch), "speed_mps", speed,
		"points", len(ev.Points), "has_audio", ev.HasAudio, "new", created)
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

// e164 is a phone number in E.164: + and 7 to 15 digits, as remoteconfig checks it.
var e164 = regexp.MustCompile(`^\+[1-9][0-9]{6,14}$`)

// maxFamilyNumbers is the app's limit (SetupForm.MAX_FAMILY_NUMBERS). Every
// alert messages each of them, so an unbounded list would let one request
// fan out to any number of strangers.
const maxFamilyNumbers = 5

func validateDevice(reg contract.DeviceRegistration) error {
	switch {
	case reg.DeviceID == "":
		return errors.New("device_id is required")
	case reg.PhoneNumber == "":
		return errors.New("phone_number is required")
	case len(reg.FamilyNumbers) > maxFamilyNumbers:
		return fmt.Errorf("family_numbers has at most %d numbers", maxFamilyNumbers)
	case len(reg.Ed25519PublicKey) != 0 && len(reg.Ed25519PublicKey) != ed25519.PublicKeySize:
		return errors.New("ed25519_public_key must be 32 bytes of standard Base64")
	}
	for _, n := range reg.FamilyNumbers {
		if !e164.MatchString(n) {
			return errors.New("family_numbers must be E.164 (+ and 7 to 15 digits)")
		}
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

// maxUserNameRunes bounds user_name: "Mamá" says it, so it is a name, not a
// paragraph.
const maxUserNameRunes = 30

// maxPhraseRunes and maxPhraseWords bound duress_phrase and safe_phrase. The
// person says them as the answer to "¿cómo vas?" in a call that has to sound
// ordinary: the app's default and the longest eval phrase are 8 words and
// under 40 characters, so the caps leave more than twice that. They also
// bound the duress detector, whose cost grows with the cube of the words when
// it compares the two phrases (two 1,000-word phrases took over 10 s of CPU)
// and with the square of the duress phrase's words when it judges a call.
// POST /devices has no auth, so both caps are checked before the detector
// ever sees a phrase.
const (
	maxPhraseRunes = 150
	maxPhraseWords = 20
)

// validateSetup checks the optional setup fields of a registration whose
// user_name and safe_phrase are already trimmed, and caps both phrases.
// Absent ("") is fine: the stored value is kept. The errors never quote a
// phrase.
func validateSetup(reg contract.DeviceRegistration) error {
	if n := utf8.RuneCountInString(reg.UserName); n > maxUserNameRunes {
		return fmt.Errorf("user_name must be 1 to %d characters", maxUserNameRunes)
	}
	if err := checkPhraseLength("duress_phrase", reg.DuressPhrase); err != nil {
		return err
	}
	if err := checkPhraseLength("safe_phrase", reg.SafePhrase); err != nil {
		return err
	}
	switch {
	case reg.SafePhrase == "":
		return nil
	case !hasWord(reg.SafePhrase):
		return errors.New("safe_phrase needs at least one word")
	case tooCloseToDuress(reg.SafePhrase, reg.DuressPhrase):
		return errors.New("safe_phrase is too close to duress_phrase: pick one that does not share most of its key words")
	}
	return nil
}

// tooCloseToDuress reports whether the duress detector would hear the safe
// phrase as the duress phrase: said alone, it is the phrase, a close variant,
// or half or more of its key words (an unclear call). It uses duress.Rules
// itself, so both share one normalization (accents, case, stopwords, typos).
func tooCloseToDuress(safe, duressPhrase string) bool {
	if !hasWord(duressPhrase) {
		return false // nothing to compare with
	}
	said := []contract.TranscriptTurn{{Role: duress.RoleUser, Message: safe}}
	res, err := duress.Rules{}.Detect(context.Background(), said, duressPhrase)
	return err != nil || res.Verdict != contract.VerdictNoDuress
}

// checkPhraseLength rejects a phrase over maxPhraseRunes or maxPhraseWords.
// The error names field and the caps, never the phrase.
func checkPhraseLength(field, phrase string) error {
	if utf8.RuneCountInString(phrase) > maxPhraseRunes || countWords(phrase) > maxPhraseWords {
		return fmt.Errorf("%s must be at most %d characters and %d words", field, maxPhraseRunes, maxPhraseWords)
	}
	return nil
}

// countWords counts the runs of letters and digits in s. The duress detector
// splits a phrase on everything else, so this is the most words it can see.
func countWords(s string) int {
	n, inWord := 0, false
	for _, r := range s {
		word := unicode.IsLetter(r) || unicode.IsDigit(r)
		if word && !inWord {
			n++
		}
		inWord = word
	}
	return n
}

// hasWord reports whether s has a letter or a digit.
func hasWord(s string) bool {
	return strings.IndexFunc(s, func(r rune) bool { return unicode.IsLetter(r) || unicode.IsDigit(r) }) >= 0
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
	if len(points) > maxPoints {
		return fmt.Errorf("points has %d fixes; send at most %d per request", len(points), maxPoints)
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
