// Package remoteconfig serves GET /config: public, no auth; the phone caches it
// (#55). It carries the central SMS number and the server public keys.
package remoteconfig

import (
	"encoding/json"
	"errors"
	"net/http"
	"regexp"

	"github.com/platanus-hack/platanus-hack-26-ve-team-27/server/internal/contract"
	"github.com/platanus-hack/platanus-hack-26-ve-team-27/server/internal/emergency/serverkeys"
)

var e164 = regexp.MustCompile(`^\+[1-9][0-9]{6,14}$`)

// CheckSMSNumber accepts an empty number (not served) or an E.164 one.
func CheckSMSNumber(n string) error {
	if n == "" || e164.MatchString(n) {
		return nil
	}
	return errors.New("remoteconfig: SMS number must be E.164")
}

// Handler answers GET /config.
type Handler struct {
	body []byte
}

// New precomputes the reply from the SMS number ("" omits it) and the keys.
func New(smsNumber string, keys serverkeys.Keys) *Handler {
	body, err := json.Marshal(contract.RemoteConfig{
		SMSNumber:              smsNumber,
		ServerEd25519PublicKey: keys.SigningPublic(),
		ServerX25519PublicKey:  keys.SealingPublic(),
	})
	if err != nil {
		panic(err) // two byte slices and a string cannot fail to marshal
	}
	return &Handler{body: body}
}

// Register mounts GET /config on mux.
func (h *Handler) Register(mux *http.ServeMux) { mux.HandleFunc("GET /config", h.get) }

func (h *Handler) get(w http.ResponseWriter, _ *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-store")
	_, _ = w.Write(h.body)
}
