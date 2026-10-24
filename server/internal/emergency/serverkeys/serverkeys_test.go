package serverkeys

import (
	"bytes"
	"crypto/ed25519"
	"crypto/rand"
	"encoding/base64"
	"reflect"
	"strings"
	"testing"

	"github.com/platanus-hack/platanus-hack-26-ve-team-27/server/internal/config"
	"github.com/platanus-hack/platanus-hack-26-ve-team-27/server/internal/emergency/payload"
)

const (
	seedB64   = "fzGoNJLP2/r66mzW0oeSXQ1Kv733SWEAwFHzDLiH3nA="
	x25519B64 = "Yz7sEqOIuAOJn5nsqy4HM0UOh6HaKojdoOJyMvtQXjw="
	wantEdPub = "/x+fnERHrOfQ9HPdLiQP9ya2eBtqDNBmsrjsP9ZnMBE="
	wantXPub  = "14/r5bky56/VN9vYfVVwD79P59iaXXdMRrOvYZxUGj0="
)

func TestFromConfig(t *testing.T) {
	const edName, xName = "EMERGENCY_ED25519_SEED", "EMERGENCY_X25519_PRIVATE_KEY"
	t.Run("both set", func(t *testing.T) {
		k, eph, err := FromConfig(config.Emergency{Ed25519Seed: seedB64, X25519PrivateKey: x25519B64}, rand.Reader)
		if err != nil || len(eph) != 0 {
			t.Fatalf("err=%v ephemeral=%v, want none", err, eph)
		}
		if got := base64.StdEncoding.EncodeToString(k.SigningPublic()); got != wantEdPub {
			t.Errorf("SigningPublic = %s, want %s", got, wantEdPub)
		}
		if got := base64.StdEncoding.EncodeToString(k.SealingPublic()); got != wantXPub {
			t.Errorf("SealingPublic = %s, want %s", got, wantXPub)
		}
	})
	t.Run("both unset", func(t *testing.T) {
		k, eph, err := FromConfig(config.Emergency{}, rand.Reader)
		if err != nil {
			t.Fatal(err)
		}
		if want := []string{edName, xName}; !reflect.DeepEqual(eph, want) {
			t.Errorf("ephemeral = %v, want %v", eph, want)
		}
		checkUsable(t, k)
	})
	t.Run("seed unset", func(t *testing.T) {
		_, eph, err := FromConfig(config.Emergency{X25519PrivateKey: x25519B64}, rand.Reader)
		if err != nil {
			t.Fatal(err)
		}
		if want := []string{edName}; !reflect.DeepEqual(eph, want) {
			t.Errorf("ephemeral = %v, want %v", eph, want)
		}
	})
	bad := map[string]string{
		"url-safe":   "fzGoNJLP2_r66mzW0oeSXQ1Kv733SWEAwFHzDLiH3nA=",
		"31 bytes":   base64.StdEncoding.EncodeToString(make([]byte, 31)),
		"not b64":    "not base64",
		"trailing":   seedB64 + "x",
		"no padding": strings.TrimRight(seedB64, "="),
	}
	for name, v := range bad {
		t.Run("bad seed "+name, func(t *testing.T) {
			_, _, err := FromConfig(config.Emergency{Ed25519Seed: v, X25519PrivateKey: x25519B64}, rand.Reader)
			checkErr(t, err, edName, v)
		})
		t.Run("bad x25519 "+name, func(t *testing.T) {
			_, _, err := FromConfig(config.Emergency{Ed25519Seed: seedB64, X25519PrivateKey: v}, rand.Reader)
			checkErr(t, err, xName, v)
		})
	}
}

func checkErr(t *testing.T, err error, name, value string) {
	t.Helper()
	if err == nil {
		t.Fatal("want an error")
	}
	if !strings.Contains(err.Error(), name) {
		t.Errorf("error %q does not name %s", err, name)
	}
	if strings.Contains(err.Error(), value) {
		t.Errorf("error %q leaks the value", err)
	}
}

func checkUsable(t *testing.T, k Keys) {
	t.Helper()
	msg := []byte("hello")
	if !ed25519.Verify(k.SigningPublic(), msg, ed25519.Sign(k.Signing, msg)) {
		t.Error("sign/verify round trip failed")
	}
	box, err := payload.Seal(rand.Reader, k.Sealing.PublicKey(), msg)
	if err != nil {
		t.Fatal(err)
	}
	got, err := payload.Open(k.Sealing, box)
	if err != nil || !bytes.Equal(got, msg) {
		t.Errorf("seal/open round trip = %q, %v", got, err)
	}
}

func TestGenerateReadsReader(t *testing.T) {
	src := make([]byte, 64)
	for i := range src {
		src[i] = byte(i)
	}
	a, err := Generate(bytes.NewReader(src))
	if err != nil {
		t.Fatal(err)
	}
	b, err := Generate(bytes.NewReader(src))
	if err != nil {
		t.Fatal(err)
	}
	if !a.Signing.Equal(b.Signing) || !a.Sealing.Equal(b.Sealing) {
		t.Error("Generate from the same bytes is not deterministic")
	}
	if _, err := Generate(bytes.NewReader(src[:10])); err == nil {
		t.Error("Generate from a 10-byte reader: want an error")
	}
}
