package voice

import (
	"crypto/sha256"
	"database/sql"
	"encoding/json"
	"errors"
	"io"
	"log"
	"net/http"
	"os"
	"regexp"
	"strconv"
	"sync"
	"time"
)

// UploadInterval is the least time between two uploads of one device.
const UploadInterval = time.Minute

// maxFormBytes bounds the whole multipart body: the audio plus room for the
// other fields and the part headers.
const maxFormBytes = MaxAudioBytes + 64<<10

// uuidPattern is the shape of a device_id (docs/contract.md).
var uuidPattern = regexp.MustCompile(`^[0-9a-fA-F]{8}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{12}$`)

// Handler serves the voice routes. Create it with NewHandler or FromEnv.
type Handler struct {
	store Store
	svc   *Service // nil: every route answers 503
	now   func() time.Time

	mu         sync.Mutex
	lastUpload map[string]time.Time
}

// NewHandler returns the voice routes on store and el. A nil store or el
// makes every route answer 503.
func NewHandler(store Store, el ElevenLabs) *Handler {
	h := &Handler{store: store, now: time.Now, lastUpload: map[string]time.Time{}}
	if store != nil && el != nil {
		h.svc = NewService(store, el)
	}
	return h
}

// FromEnv returns the voice routes on pool and the ElevenLabs key in
// ELEVENLABS_API_KEY. pool may be nil (no DATABASE_URL) and the key unset:
// then every route answers 503.
func FromEnv(pool *sql.DB) *Handler {
	key := os.Getenv("ELEVENLABS_API_KEY")
	if pool == nil || key == "" {
		return NewHandler(nil, nil)
	}
	return NewHandler(NewDBStore(pool), &Client{APIKey: key})
}

// Register adds the routes to mux.
func (h *Handler) Register(mux *http.ServeMux) {
	mux.HandleFunc("POST /devices/{id}/voice", h.upload)
	mux.HandleFunc("DELETE /devices/{id}/voice", h.remove)
	mux.HandleFunc("GET /devices/{id}/voice/preview", h.preview)
}

// device answers the shared failures (bad id, no service, unknown device)
// and returns the device, or ok=false when it already answered.
func (h *Handler) device(w http.ResponseWriter, r *http.Request) (id string, d Device, ok bool) {
	id = r.PathValue("id")
	if !uuidPattern.MatchString(id) {
		writeError(w, http.StatusNotFound, "unknown device")
		return "", Device{}, false
	}
	if h.svc == nil {
		writeError(w, http.StatusServiceUnavailable, "voice is not configured on this server")
		return "", Device{}, false
	}
	d, err := h.store.Device(r.Context(), id)
	switch {
	case errors.Is(err, ErrUnknownDevice):
		writeError(w, http.StatusNotFound, "unknown device")
		return "", Device{}, false
	case err != nil:
		internalError(w, "read device", err)
		return "", Device{}, false
	}
	return id, d, true
}

// authorize checks the signature: 409 without a bound key, 401 otherwise.
func (h *Handler) authorize(w http.ResponseWriter, r *http.Request, id string, d Device, audioSHA256 []byte) bool {
	err := verify(d.PublicKey, r.Header.Get(HeaderSignedAt), r.Header.Get(HeaderSignature), func(at int64) []byte {
		return SignedMessage(r.Method, id, at, audioSHA256)
	}, h.now())
	switch {
	case errors.Is(err, errNoKey):
		writeError(w, http.StatusConflict, err.Error())
		return false
	case err != nil:
		writeError(w, http.StatusUnauthorized, err.Error())
		return false
	}
	return true
}

func (h *Handler) upload(w http.ResponseWriter, r *http.Request) {
	if h.svc == nil {
		// Before reading the body: nothing would use it.
		h.device(w, r)
		return
	}
	form, status, msg := readForm(w, r)
	if status != 0 {
		writeError(w, status, msg)
		return
	}
	id, d, ok := h.device(w, r)
	if !ok {
		return
	}
	sum := sha256.Sum256(form.audio)
	if !h.authorize(w, r, id, d, sum[:]) {
		return
	}
	if wait := h.claimUpload(id); wait > 0 {
		w.Header().Set("Retry-After", strconv.Itoa(int(wait.Round(time.Second)/time.Second)))
		writeError(w, http.StatusTooManyRequests, "one voice upload per minute")
		return
	}
	voiceID, err := h.svc.Set(r.Context(), id, NewVoice{RemoveBackgroundNoise: true}, Sample{Audio: form.audio, Format: form.format})
	if err != nil {
		h.upstreamOr500(w, "set voice", id, err)
		return
	}
	log.Printf("voice: device %s now speaks with voice %s", DefaultName(id), voiceID)
	writeJSON(w, http.StatusCreated, map[string]string{"voice_id": voiceID})
}

func (h *Handler) remove(w http.ResponseWriter, r *http.Request) {
	id, d, ok := h.device(w, r)
	if !ok || !h.authorize(w, r, id, d, nil) {
		return
	}
	if err := h.svc.Remove(r.Context(), id); err != nil {
		h.upstreamOr500(w, "remove voice", id, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (h *Handler) preview(w http.ResponseWriter, r *http.Request) {
	id, d, ok := h.device(w, r)
	if !ok {
		return
	}
	if d.VoiceID == "" {
		writeError(w, http.StatusNotFound, "device has no voice of its own")
		return
	}
	mp3, err := h.svc.Preview(r.Context(), id)
	switch {
	case errors.Is(err, ErrNoVoice), errors.Is(err, ErrUnknownDevice):
		writeError(w, http.StatusNotFound, "device has no voice of its own")
		return
	case err != nil:
		h.upstreamOr500(w, "preview voice", id, err)
		return
	}
	w.Header().Set("Content-Type", "audio/mpeg")
	w.Header().Set("Content-Length", strconv.Itoa(len(mp3)))
	w.Header().Set("Cache-Control", "no-store")
	_, _ = w.Write(mp3)
}

// upstreamOr500 answers 502 for an ElevenLabs failure, naming a refused key
// (it needs the voices write permission), and 500 for anything else.
func (h *Handler) upstreamOr500(w http.ResponseWriter, what, id string, err error) {
	if !errors.Is(err, ErrElevenLabs) {
		internalError(w, what, err)
		return
	}
	log.Printf("voice: %s for device %s: %v", what, DefaultName(id), err)
	msg := "ElevenLabs failed; the device keeps its previous voice"
	if errors.Is(err, ErrAuth) {
		msg = "ElevenLabs refused the server's API key (it needs the Voices write permission)"
	}
	writeError(w, http.StatusBadGateway, msg)
}

// claimUpload records an upload for the device now, or returns how long it
// must wait when its last one was under UploadInterval ago.
func (h *Handler) claimUpload(id string) time.Duration {
	now := h.now()
	h.mu.Lock()
	defer h.mu.Unlock()
	if last, ok := h.lastUpload[id]; ok && now.Sub(last) < UploadInterval {
		return UploadInterval - now.Sub(last)
	}
	for k, t := range h.lastUpload {
		if now.Sub(t) >= UploadInterval {
			delete(h.lastUpload, k)
		}
	}
	h.lastUpload[id] = now
	return 0
}

type uploadForm struct {
	audio  []byte
	format Format
}

// readForm reads the multipart body part by part into memory: unlike
// ParseMultipartForm it never spills a large file to a temp file, so the
// audio is never on disk. status is 0 when the form is good.
func readForm(w http.ResponseWriter, r *http.Request) (f uploadForm, status int, msg string) {
	r.Body = http.MaxBytesReader(w, r.Body, maxFormBytes)
	mr, err := r.MultipartReader()
	if err != nil {
		return f, http.StatusBadRequest, "body must be multipart/form-data with audio and consent"
	}
	var consent string
	var haveAudio bool
	for {
		part, err := mr.NextPart()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return f, readStatus(err), "malformed multipart body"
		}
		switch part.FormName() {
		case "audio":
			f.audio, err = io.ReadAll(io.LimitReader(part, MaxAudioBytes+1))
			haveAudio = true
			if err == nil && len(f.audio) > MaxAudioBytes {
				return f, http.StatusRequestEntityTooLarge, "audio is over 10 MB"
			}
		case "consent":
			var b []byte
			b, err = io.ReadAll(io.LimitReader(part, 16))
			consent = string(b)
		default:
			_, err = io.Copy(io.Discard, part)
		}
		if err != nil {
			return f, readStatus(err), "could not read the body"
		}
	}
	if consent != "true" {
		return f, http.StatusBadRequest, "consent=true is required: the voice's owner must agree to the clone"
	}
	if !haveAudio || len(f.audio) == 0 {
		return f, http.StatusBadRequest, "audio is required"
	}
	format, ok := Sniff(f.audio)
	if !ok {
		return f, http.StatusBadRequest, "audio must be mp3, wav, m4a, ogg or webm"
	}
	f.format = format
	return f, 0, ""
}

func readStatus(err error) int {
	var tooBig *http.MaxBytesError
	if errors.As(err, &tooBig) {
		return http.StatusRequestEntityTooLarge
	}
	return http.StatusBadRequest
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

func writeError(w http.ResponseWriter, status int, msg string) {
	writeJSON(w, status, map[string]string{"error": msg})
}

// internalError logs err and answers 500 without leaking it.
func internalError(w http.ResponseWriter, what string, err error) {
	log.Printf("voice: %s: %v", what, err)
	writeError(w, http.StatusInternalServerError, "internal error")
}
