package ingest

import (
	"encoding/json"
	"errors"
	"io"
	"log"
	"net/http"

	"github.com/platanus-hack/platanus-hack-26-ve-team-27/server/internal/emergency/payload"
)

// maxBodyBytes caps both routes: the SMS text is 137 chars, the mesh text 207.
const maxBodyBytes = 1 << 10

// Handler serves the offline emergency ingest routes (#57):
//
//	POST /emergencies        body: the SMS text "MAMA1 <b64url>" (any Content-Type)
//	POST /emergencies/relay  body: JSON {"text":"MAMA1:SEAL:<b64>","hops":N}
//
// Success (new or duplicate) is 200 text/plain with the signed ack text
// "MAMA1:ACK:<b64>", verbatim so a relay can rebroadcast it. Errors are JSON
// {"error":"..."}:
//
//	400 malformed payload or text, bad JSON, bad hops, wrong box (permanent)
//	403 signature does not verify (permanent)
//	409 emergency_id belongs to another device (permanent)
//	413 body over 1 KiB (permanent)
//	422 device not registered; POST /devices first (permanent)
//	503 database not configured (retry later)
//	500 anything else (retry later)
//
// Every 4xx is permanent: neither the app nor a relay should retry it.
type Handler struct {
	svc *Service
}

// NewHandler returns the handler. A nil svc means no database: every route
// answers 503.
func NewHandler(svc *Service) *Handler { return &Handler{svc: svc} }

// RelayUpload is the body of POST /emergencies/relay. Hops is the number of
// mesh relays between the emitter and the uploader (0 = heard directly);
// omitted means unknown.
type RelayUpload struct {
	Text string `json:"text"`
	Hops *int   `json:"hops,omitempty"`
}

// Register mounts the routes on mux.
func (h *Handler) Register(mux *http.ServeMux) {
	mux.HandleFunc("POST /emergencies", h.handleEmergency)
	mux.HandleFunc("POST /emergencies/relay", h.handleRelay)
}

func (h *Handler) handleEmergency(w http.ResponseWriter, r *http.Request) {
	if !h.ready(w) {
		return
	}
	body, err := io.ReadAll(http.MaxBytesReader(w, r.Body, maxBodyBytes))
	if err != nil {
		writeBodyError(w, err)
		return
	}
	signed, err := payload.ParseSMSText(string(body))
	if err != nil {
		writeIngestError(w, r, err)
		return
	}
	res, err := h.svc.Ingest(r.Context(), signed, ChannelHTTP, 0)
	if err != nil {
		writeIngestError(w, r, err)
		return
	}
	writeAck(w, res)
}

func (h *Handler) handleRelay(w http.ResponseWriter, r *http.Request) {
	if !h.ready(w) {
		return
	}
	var up RelayUpload
	dec := json.NewDecoder(http.MaxBytesReader(w, r.Body, maxBodyBytes))
	dec.DisallowUnknownFields()
	err := dec.Decode(&up)
	if err == nil {
		if _, extra := dec.Token(); extra != io.EOF {
			err = errors.New("trailing data after the JSON object")
		}
	}
	if err != nil {
		var tooBig *http.MaxBytesError
		if errors.As(err, &tooBig) {
			writeBodyError(w, err)
			return
		}
		writeError(w, http.StatusBadRequest, "invalid JSON: "+err.Error())
		return
	}
	hops := HopsUnknown
	if up.Hops != nil {
		hops = *up.Hops
		if hops < 0 || hops > MaxHops {
			writeError(w, http.StatusBadRequest, "hops must be between 0 and 255")
			return
		}
	}
	res, err := h.svc.IngestMesh(r.Context(), up.Text, hops)
	if err != nil {
		writeIngestError(w, r, err)
		return
	}
	writeAck(w, res)
}

func (h *Handler) ready(w http.ResponseWriter) bool {
	if h.svc == nil {
		writeError(w, http.StatusServiceUnavailable, "database not configured")
		return false
	}
	return true
}

func writeAck(w http.ResponseWriter, res Result) {
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write([]byte(payload.MeshAckText(res.Ack)))
}

func writeBodyError(w http.ResponseWriter, err error) {
	var tooBig *http.MaxBytesError
	if errors.As(err, &tooBig) {
		writeError(w, http.StatusRequestEntityTooLarge, "body too large")
		return
	}
	writeError(w, http.StatusBadRequest, "cannot read the body: "+err.Error())
}

// writeIngestError maps a service error to its status.
func writeIngestError(w http.ResponseWriter, r *http.Request, err error) {
	switch {
	case errors.Is(err, ErrUnknownDevice):
		writeError(w, http.StatusUnprocessableEntity, "device not registered; send POST /devices with ed25519_public_key first")
	case errors.Is(err, ErrConflict):
		writeError(w, http.StatusConflict, "emergency_id belongs to another device")
	case errors.Is(err, payload.ErrSignature):
		writeError(w, http.StatusForbidden, "signature does not verify")
	case errors.Is(err, payload.ErrFormat), errors.Is(err, payload.ErrDecrypt):
		writeError(w, http.StatusBadRequest, err.Error())
	default:
		// A client that went away cancels the request context: not a server fault.
		if r.Context().Err() == nil {
			log.Printf("ingest: %v", err)
		}
		writeError(w, http.StatusInternalServerError, "internal error")
	}
}

func writeError(w http.ResponseWriter, status int, msg string) {
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(map[string]string{"error": msg})
}
