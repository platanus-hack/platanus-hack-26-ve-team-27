# 0001. Offline emergency payload

## Status

Accepted, 2026-10-24. Ticket #54, spec #62.

## Context

When the phone has no data connection, MAMÁ still has to tell the server that
someone is in danger. Two offline channels exist: an SMS to a server number
(about 160 characters, readable by the carrier) and a bitchat mesh message
relayed by nearby phones until one has internet. Both are tiny and untrusted,
so the payload must be compact, must prove that it comes from a registered
device, and (on the mesh) must not expose the location to relays.

The server (Go) and the phone (Kotlin) must agree on every byte. Go is the
reference implementation in `server/internal/emergency/payload`; the Kotlin
port lives in `app/src/main/java/com/team27/mama/emergency/payload/`. Both are
pinned by one shared file, `server/internal/emergency/payload/testdata/vectors.json`.

## Decision

All multi-byte integers are big-endian and unsigned, except `lat_e5` and
`lon_e5`, which are two's-complement int32.

### Body (34 bytes)

| Offset | Size | Field | Rule |
|---|---|---|---|
| 0 | 1 | version | must be `1` |
| 1 | 1 | kind | `1` alert, `2` update, `3` cancel (`4` is ack-only and rejected here) |
| 2 | 8 | device_id | first 8 bytes of SHA-256 of the device Ed25519 public key (32 raw bytes) |
| 10 | 8 | emergency_id | random 64-bit value, the same for the alert, updates and cancel of one emergency |
| 18 | 2 | seq (uint16) | 0 for the first alert, +1 per update or cancel |
| 20 | 4 | lat_e5 (int32) | degrees x 1e5, in [-9000000, 9000000] |
| 24 | 4 | lon_e5 (int32) | degrees x 1e5, in [-18000000, 18000000] |
| 28 | 2 | loc_age_s (uint16) | seconds since the fix, `0..65534` (saturates at 65534); `65535` means no fix |
| 30 | 4 | timestamp (uint32) | unix seconds |

No fix: `loc_age_s = 65535` and then `lat_e5` and `lon_e5` must both be 0.
Anything else with 65535 is a format error. A first alert or a cancel may be
sent before the phone has a GPS fix, and the sentinel keeps those messages valid.

Degrees become E5 with `round_half_away_from_zero(deg * 1e5)` on the double
product (Go `math.Round`; Kotlin `BigDecimal(deg * 1e5).setScale(0, HALF_UP)`,
not `Math.round`, which differs on negative halves). NaN, infinities and
`|deg| > 180` are rejected. The `degrees` vectors pin this.

### Signed payload (98 bytes)

Signed payload = body (34) followed by a pure Ed25519 signature (64, RFC 8032,
no prehash) over the message `SignatureContext || body`, where
`SignatureContext` is the 25 ASCII bytes `MAMA-offline-emergency-v1`. The
context is not transmitted.

Why a context: the device key is bitchat's Ed25519 identity key, which also
signs bitchat packets and announcements. The ASCII context means a MAMÁ
signature is never valid as a signature over a bitchat packet (binary, starting
with a version byte 0x01 or 0x02), and the reverse, at zero wire cost.

`device_id` is the first 8 bytes of SHA-256 of the public key, so the server
can find the registered key from the body alone before it verifies. Verify
order, in both languages: length, version, kind, field ranges, `device_id`
against the given public key, signature.

### Ack (76 bytes)

`version (1) || kind=4 (1) || emergency_id (8) || seq (2) ||` Ed25519 signature
(64) by the server key over `SignatureContext ||` the first 12 bytes. `seq` is
the highest seq the server has accepted for that `emergency_id`. Acks are not
encrypted.

### Sealed box (mesh only)

`ephemeral_x25519_public (32) || AES-256-GCM ciphertext || tag (16)`. The
overhead is 48 bytes, so a sealed signed payload is 146 bytes.

1. `eph` is 32 random bytes used as an X25519 private key (the library clamps);
   `eph_pub = X25519(eph, basepoint)`.
2. `shared = X25519(eph, recipient_x25519_public)`. An all-zero result is an error.
3. `okm = HKDF-SHA256(IKM = shared, salt = eph_pub || recipient_x25519_public`
   (64 bytes)`, info = "MAMA-sealed-box-v1", length 44)`. `key = okm[0:32]`,
   `nonce = okm[32:44]`. The nonce is not transmitted; there is one key per
   ephemeral, so it is never reused.
4. `ciphertext || tag = AES-256-GCM-Seal(key, nonce, plaintext, AAD = empty)`.

Open: a box shorter than 48 bytes is a format error; an all-zero shared secret
(low-order ephemeral key) or a GCM authentication failure is a decrypt error.

The server has a separate X25519 key pair for the sealed box. It is not derived
from its Ed25519 key, because the Go standard library has no Ed25519 to X25519
conversion. #55 delivers both server public keys in the remote config.

### Transport texts

Prefixes are exact and case-sensitive.

| Text | Format | Length |
|---|---|---|
| SMS payload | `MAMA1 ` + Base64url without padding (RFC 4648 section 5) of the 98-byte signed payload | 6 + 131 = 137 |
| SMS ack | `MAMA1 ` + Base64url without padding of the 76-byte ack | 6 + 102 = 108 |
| Mesh payload | `MAMA1:SEAL:` + standard Base64 with padding (RFC 4648 section 4) of the 146-byte sealed box | 11 + 196 = 207 |
| Mesh ack | `MAMA1:ACK:` + standard Base64 with padding of the 76-byte ack | 10 + 104 = 114 |

Every mesh text starts with `MAMA1:` so a future MeshGateway can filter on it;
the tag between the first and second `:` says which kind it is. `:` is in
neither Base64 alphabet, so the split is unambiguous. The receiver of an SMS
tells payload from ack by length and kind byte (each side knows what it expects).

Parsing: strip leading and trailing characters from `{space, tab, CR, LF}` only
(providers append newlines), require the exact prefix, decode, then re-encode and
require equality with the input. That rejects padding in SMS, missing padding in
mesh, embedded newlines (Go decoders skip them) and non-canonical trailing
bits. An unknown mesh tag is a format error.

### Error categories

Three categories, the same in Go (`ErrFormat`, `ErrSignature`, `ErrDecrypt`,
checked with `errors.Is`) and Kotlin (`PayloadException.Reason`):

- `format`: wrong length, version, kind, range, prefix, Base64, mesh tag, or a sealed box that is too short.
- `signature`: bad signature, `device_id` not matching the public key, or a public key that is not 32 bytes.
- `decrypt`: the sealed box cannot be opened.

Decoders never throw or panic with anything else on hostile input.

### Shared vectors

`server/internal/emergency/payload/testdata/vectors.json` holds keys (derived
from SHA-256 of fixed ASCII labels), degree conversions, four payloads with their
body, signed bytes, SMS, sealed box and mesh text, two acks, and invalid inputs
with the expected error category. It is generated by Go and never edited by hand:

```
cd server && go test ./internal/emergency/payload/ -run TestVectors -update
```

Without `-update`, `TestVectors` fails if the file differs from what the code
produces (compared as decoded JSON, so CRLF does not matter). The Kotlin tests
read the same file.

## Changes vs #62 section 3

a. The signature covers a 25-byte domain-separation context plus the body, not the body alone.
b. `loc_age_s = 65535` means "no fix", with `lat_e5` and `lon_e5` equal to 0.
c. The ack layout adds `version` and `seq` (`version || kind=4 || emergency_id || seq || sig`, 76 bytes) so the emitter knows which update or cancel was received.
d. Mesh texts carry a `SEAL` or `ACK` tag after `MAMA1:`.
e. The server has a dedicated X25519 key for the sealed box.

## Consequences

- The codec does no clock or replay checks. The server dedups by `seq` within an emergency (#57).
- Emergencies are keyed by (`device_id`, `emergency_id`). `emergency_id` travels in clear in SMS and mesh acks, so an update or cancel is accepted only from the `device_id` that sent the alert (#57 enforces it).
- An SMS is readable by the carrier and the SMS provider, by design: it is signed, not encrypted.
- Mesh relays cannot read the payload: they see the `MAMA1:` prefix, the tag and the length. They are not anonymous, though: the bitchat packet carries the originator's sender identity (the same Ed25519 key as the device, so linkable to `device_id`), and mesh acks expose `emergency_id` and `seq`.
- A relay cannot match a mesh ack to the `MAMA1:SEAL:` texts it carries, because `emergency_id` is inside the sealed box. Acks therefore do not stop mesh propagation by themselves: relays keep forwarding until the bitchat TTL or gossip expiry, and the server's dedup absorbs the duplicates. A relay may drop a pending upload when the server's answer to its own upload is the ack. A cleartext correlator was considered and rejected, because it would let relays link updates of one emergency; a future MAMA2 could add it.
- There is no forward secrecy against the server's X25519 key: if it leaks, past mesh captures can be read.
- A format change means `version 2` and new `MAMA2` prefixes.
