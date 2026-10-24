package payload

import (
	"bytes"
	"crypto/ecdh"
	"crypto/ed25519"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"os"
	"reflect"
	"testing"
)

var update = flag.Bool("update", false, "rewrite testdata/vectors.json")

const vectorsPath = "testdata/vectors.json"
const regenerate = "run go test ./internal/emergency/payload/ -run TestVectors -update"

type vectorKeys struct {
	DeviceSeed    string `json:"device_ed25519_seed"`
	DevicePublic  string `json:"device_ed25519_public"`
	DeviceID      string `json:"device_id"`
	ServerSeed    string `json:"server_ed25519_seed"`
	ServerPublic  string `json:"server_ed25519_public"`
	ServerXPriv   string `json:"server_x25519_private"`
	ServerXPublic string `json:"server_x25519_public"`
}

type vectorDegree struct {
	Deg float64 `json:"deg"`
	E5  int32   `json:"e5"`
}

type vectorPayload struct {
	Name        string `json:"name"`
	Kind        int    `json:"kind"`
	EmergencyID string `json:"emergency_id"`
	Seq         int    `json:"seq"`
	LatE5       int32  `json:"lat_e5"`
	LonE5       int32  `json:"lon_e5"`
	LocAgeS     int    `json:"loc_age_s"`
	Timestamp   uint32 `json:"timestamp"`
	Body        string `json:"body"`
	Signed      string `json:"signed"`
	SMS         string `json:"sms"`
	EphPrivate  string `json:"ephemeral_x25519_private"`
	Sealed      string `json:"sealed"`
	Mesh        string `json:"mesh"`
}

type vectorAck struct {
	Name        string `json:"name"`
	EmergencyID string `json:"emergency_id"`
	Seq         int    `json:"seq"`
	Ack         string `json:"ack"`
	SMS         string `json:"sms"`
	Mesh        string `json:"mesh"`
}

type vectorInvalid struct {
	Name  string `json:"name"`
	Op    string `json:"op"`
	Input string `json:"input"`
	Error string `json:"error"`
}

type vectorFile struct {
	Keys     vectorKeys      `json:"keys"`
	Degrees  []vectorDegree  `json:"degrees"`
	Payloads []vectorPayload `json:"payloads"`
	Acks     []vectorAck     `json:"acks"`
	Invalid  []vectorInvalid `json:"invalid"`
}

func labelSeed(label string) []byte {
	s := sha256.Sum256([]byte(label))
	return s[:]
}

func hex64(id uint64) string {
	return fmt.Sprintf("%016x", id)
}

func flip(b []byte, i int) []byte {
	c := append([]byte{}, b...)
	c[i] ^= 0x01
	return c
}

func buildVectors() vectorFile {
	dev, srv := deviceKey(), serverKey()
	xpriv, err := ecdh.X25519().NewPrivateKey(labelSeed("mama-test-server-x25519"))
	if err != nil {
		panic(err)
	}
	devID := DeviceID(pub(dev))
	v := vectorFile{Keys: vectorKeys{
		DeviceSeed:    hex.EncodeToString(dev.Seed()),
		DevicePublic:  hex.EncodeToString(pub(dev)),
		DeviceID:      hex64(devID),
		ServerSeed:    hex.EncodeToString(srv.Seed()),
		ServerPublic:  hex.EncodeToString(pub(srv)),
		ServerXPriv:   hex.EncodeToString(xpriv.Bytes()),
		ServerXPublic: hex.EncodeToString(xpriv.PublicKey().Bytes()),
	}}

	for _, d := range []float64{10.48801, -66.87919, -33.44889, -70.66926, 0.000005, -0.000005, 89.999995, -179.999995, 0} {
		e5, err := ToE5(d)
		if err != nil {
			panic(err)
		}
		v.Degrees = append(v.Degrees, vectorDegree{Deg: d, E5: e5})
	}

	const eid1, eid2 = 0x0123456789abcdef, 0xfedcba9876543210
	specs := []struct {
		name string
		p    Payload
	}{
		{"alert_caracas", Payload{KindAlert, devID, eid1, 0, 1048801, -6687919, 42, 1792836000}},
		{"update_caracas", Payload{KindUpdate, devID, eid1, 1, 1048912, -6687805, 3, 1792836030}},
		{"cancel_no_fix", Payload{KindCancel, devID, eid1, 2, 0, 0, 65535, 1792836090}},
		{"alert_extremes", Payload{KindAlert, devID, eid2, 65535, -9000000, 18000000, 65534, 4294967295}},
	}
	var signed, sealed []byte
	for i, s := range specs {
		body, err := s.p.MarshalBody()
		if err != nil {
			panic(err)
		}
		sg, err := Sign(s.p, dev)
		if err != nil {
			panic(err)
		}
		eph := labelSeed("mama-test-ephemeral-" + s.name)
		sl, err := Seal(bytes.NewReader(eph), xpriv.PublicKey(), sg)
		if err != nil {
			panic(err)
		}
		if i == 0 {
			signed, sealed = sg, sl
		}
		v.Payloads = append(v.Payloads, vectorPayload{
			Name: s.name, Kind: int(s.p.Kind), EmergencyID: hex64(s.p.EmergencyID), Seq: int(s.p.Seq),
			LatE5: s.p.LatE5, LonE5: s.p.LonE5, LocAgeS: int(s.p.LocAgeS), Timestamp: s.p.Timestamp,
			Body: hex.EncodeToString(body), Signed: hex.EncodeToString(sg), SMS: SMSText(sg),
			EphPrivate: hex.EncodeToString(eph), Sealed: hex.EncodeToString(sl), Mesh: MeshSealedText(sl),
		})
	}

	var ack []byte
	for _, a := range []struct {
		name string
		a    Ack
	}{
		{"ack_alert", Ack{eid1, 0}},
		{"ack_high_bits", Ack{eid2, 65535}},
	} {
		b := SignAck(a.a, srv)
		if ack == nil {
			ack = b
		}
		v.Acks = append(v.Acks, vectorAck{
			Name: a.name, EmergencyID: hex64(a.a.EmergencyID), Seq: int(a.a.Seq),
			Ack: hex.EncodeToString(b), SMS: SMSText(b), Mesh: MeshAckText(b),
		})
	}

	base := specs[0].p
	signRaw := func(body []byte) string {
		sig := ed25519.Sign(dev, signedMessage(body))
		return hex.EncodeToString(append(append([]byte{}, body...), sig...))
	}
	with := func(f func(p *Payload)) Payload {
		p := base
		f(&p)
		return p
	}
	sms := SMSText(signed)
	lowOrder := append([]byte{}, sealed...)
	for i := 0; i < 32; i++ {
		lowOrder[i] = 0
	}

	add := func(name, op, input, errName string) {
		v.Invalid = append(v.Invalid, vectorInvalid{name, op, input, errName})
	}
	add("sms_flipped_signature", "sms_payload", SMSText(flip(signed, 97)), "signature")
	add("sms_flipped_timestamp", "sms_payload", SMSText(flip(signed, 33)), "signature")
	add("sms_padded", "sms_payload", SMSPrefix+base64.URLEncoding.EncodeToString(signed), "format")
	add("sms_newline_inside", "sms_payload", sms[:60]+"\n"+sms[60:], "format")
	add("sms_wrong_prefix", "sms_payload", "MAMA2 "+base64.RawURLEncoding.EncodeToString(signed), "format")
	add("signed_truncated", "signed_hex", hex.EncodeToString(signed[:97]), "format")
	add("signed_bad_version", "signed_hex", signRaw(rawBody(2, 1, base)), "format")
	add("signed_kind_ack", "signed_hex", signRaw(rawBody(1, 4, base)), "format")
	add("signed_lat_out_of_range", "signed_hex", signRaw(rawBody(1, 1, with(func(p *Payload) { p.LatE5 = 9000001 }))), "format")
	add("signed_no_fix_with_coords", "signed_hex", signRaw(rawBody(1, 1, with(func(p *Payload) { p.LocAgeS = 65535; p.LatE5 = 1 }))), "format")
	add("signed_other_device_id", "signed_hex", signRaw(rawBody(1, 1, with(func(p *Payload) { p.DeviceID ^= 1 }))), "signature")
	add("mesh_tampered_ciphertext", "mesh_payload", MeshSealedText(flip(sealed, 40)), "decrypt")
	add("mesh_low_order_ephemeral", "mesh_payload", MeshSealedText(lowOrder), "decrypt")
	add("mesh_short_box", "mesh_payload", MeshSealedText(sealed[:47]), "format")
	add("mesh_unknown_tag", "mesh_payload", "MAMA1:XYZ:"+base64.StdEncoding.EncodeToString(sealed), "format")
	add("mesh_ack_given_sealed", "mesh_ack", MeshSealedText(sealed), "format")
	add("mesh_ack_flipped_signature", "mesh_ack", MeshAckText(flip(ack, 75)), "signature")
	add("sms_ack_signed_by_device", "sms_ack", SMSText(SignAck(Ack{eid1, 0}, dev)), "signature")
	return v
}

func TestVectors(t *testing.T) {
	want := buildVectors()
	if *update {
		out, err := json.MarshalIndent(want, "", "  ")
		if err != nil {
			t.Fatal(err)
		}
		if err := os.MkdirAll("testdata", 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(vectorsPath, append(out, '\n'), 0o644); err != nil {
			t.Fatal(err)
		}
		return
	}
	raw, err := os.ReadFile(vectorsPath)
	if err != nil {
		t.Fatalf("%v: %s", err, regenerate)
	}
	var got vectorFile
	if err := json.Unmarshal(raw, &got); err != nil {
		t.Fatalf("%v: %s", err, regenerate)
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("testdata/vectors.json is out of date: %s", regenerate)
	}
}

func mustHex(t *testing.T, s string) []byte {
	t.Helper()
	b, err := hex.DecodeString(s)
	if err != nil {
		t.Fatal(err)
	}
	return b
}

// runInvalid is the dispatcher both languages implement.
func runInvalid(t *testing.T, c vectorInvalid, k vectorKeys) error {
	t.Helper()
	devPub := ed25519.PublicKey(mustHex(t, k.DevicePublic))
	srvPub := ed25519.PublicKey(mustHex(t, k.ServerPublic))
	xpriv, err := ecdh.X25519().NewPrivateKey(mustHex(t, k.ServerXPriv))
	if err != nil {
		t.Fatal(err)
	}
	switch c.Op {
	case "sms_payload":
		b, err := ParseSMSText(c.Input)
		if err != nil {
			return err
		}
		_, err = Verify(b, devPub)
		return err
	case "sms_ack":
		b, err := ParseSMSText(c.Input)
		if err != nil {
			return err
		}
		_, err = VerifyAck(b, srvPub)
		return err
	case "mesh_payload":
		f, err := ParseMeshText(c.Input)
		if err != nil {
			return err
		}
		if f.Type != MeshSealed {
			return fmt.Errorf("%w: not a SEAL text", ErrFormat)
		}
		b, err := Open(xpriv, f.Bytes)
		if err != nil {
			return err
		}
		_, err = Verify(b, devPub)
		return err
	case "mesh_ack":
		f, err := ParseMeshText(c.Input)
		if err != nil {
			return err
		}
		if f.Type != MeshAck {
			return fmt.Errorf("%w: not an ACK text", ErrFormat)
		}
		_, err = VerifyAck(f.Bytes, srvPub)
		return err
	case "signed_hex":
		_, err := Verify(mustHex(t, c.Input), devPub)
		return err
	}
	t.Fatalf("unknown op %q", c.Op)
	return nil
}

func TestVectorsDecode(t *testing.T) {
	raw, err := os.ReadFile(vectorsPath)
	if err != nil {
		t.Fatalf("%v: %s", err, regenerate)
	}
	var v vectorFile
	if err := json.Unmarshal(raw, &v); err != nil {
		t.Fatal(err)
	}
	devPub := ed25519.PublicKey(mustHex(t, v.Keys.DevicePublic))
	srvPub := ed25519.PublicKey(mustHex(t, v.Keys.ServerPublic))
	xpriv, err := ecdh.X25519().NewPrivateKey(mustHex(t, v.Keys.ServerXPriv))
	if err != nil {
		t.Fatal(err)
	}
	if v.Keys.DeviceID != hex64(DeviceID(devPub)) {
		t.Fatal("device_id is not the hash of the device public key")
	}
	if len(v.Payloads) == 0 || len(v.Acks) == 0 || len(v.Invalid) == 0 || len(v.Degrees) == 0 {
		t.Fatal("vectors.json has an empty section")
	}

	for _, d := range v.Degrees {
		got, err := ToE5(d.Deg)
		if err != nil || got != d.E5 {
			t.Errorf("ToE5(%v) = %d, %v; want %d", d.Deg, got, err, d.E5)
		}
	}
	for _, p := range v.Payloads {
		t.Run(p.Name, func(t *testing.T) {
			signed := mustHex(t, p.Signed)
			got, err := Verify(signed, devPub)
			if err != nil {
				t.Fatal(err)
			}
			if int(got.Kind) != p.Kind || hex64(got.EmergencyID) != p.EmergencyID || int(got.Seq) != p.Seq ||
				got.LatE5 != p.LatE5 || got.LonE5 != p.LonE5 || int(got.LocAgeS) != p.LocAgeS ||
				got.Timestamp != p.Timestamp || hex64(got.DeviceID) != v.Keys.DeviceID {
				t.Errorf("fields = %+v, vector = %+v", got, p)
			}
			if !bytes.Equal(signed[:BodySize], mustHex(t, p.Body)) {
				t.Error("body is not the prefix of signed")
			}
			opened, err := Open(xpriv, mustHex(t, p.Sealed))
			if err != nil || !bytes.Equal(opened, signed) {
				t.Errorf("Open(sealed) = %x, %v", opened, err)
			}
			if b, err := ParseSMSText(p.SMS); err != nil || !bytes.Equal(b, signed) {
				t.Errorf("ParseSMSText = %x, %v", b, err)
			}
			if f, err := ParseMeshText(p.Mesh); err != nil || f.Type != MeshSealed || !bytes.Equal(f.Bytes, mustHex(t, p.Sealed)) {
				t.Errorf("ParseMeshText = %+v, %v", f, err)
			}
			if len(p.SMS) != 137 || len(p.Mesh) != 207 {
				t.Errorf("text lengths = %d, %d; want 137, 207", len(p.SMS), len(p.Mesh))
			}
		})
	}
	for _, a := range v.Acks {
		t.Run(a.Name, func(t *testing.T) {
			check := func(got Ack, err error) {
				if err != nil || hex64(got.EmergencyID) != a.EmergencyID || int(got.Seq) != a.Seq {
					t.Errorf("ack = %+v, %v; want %s/%d", got, err, a.EmergencyID, a.Seq)
				}
			}
			check(VerifyAck(mustHex(t, a.Ack), srvPub))
			b, err := ParseSMSText(a.SMS)
			if err != nil {
				t.Fatal(err)
			}
			check(VerifyAck(b, srvPub))
			f, err := ParseMeshText(a.Mesh)
			if err != nil || f.Type != MeshAck {
				t.Fatalf("ParseMeshText = %+v, %v", f, err)
			}
			check(VerifyAck(f.Bytes, srvPub))
			if len(a.Mesh) != 114 {
				t.Errorf("ack mesh length = %d, want 114", len(a.Mesh))
			}
		})
	}
	for _, c := range v.Invalid {
		t.Run(c.Name, func(t *testing.T) {
			err := runInvalid(t, c, v.Keys)
			var want error
			switch c.Error {
			case "format":
				want = ErrFormat
			case "signature":
				want = ErrSignature
			case "decrypt":
				want = ErrDecrypt
			default:
				t.Fatalf("unknown error %q", c.Error)
			}
			if !errors.Is(err, want) {
				t.Errorf("error = %v, want %v", err, want)
			}
		})
	}
}
