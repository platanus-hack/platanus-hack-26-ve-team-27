# Contract: shapes shared by the phone, the server, the call and the notifier

Frozen at the contract freeze (Sat Oct 24, 07:05 VET, issue #9). After the
freeze, any change to a shape, a threshold or the schema needs the Tech
lead's OK, and it changes Go, Kotlin, the sample and this page together.

| What | Where |
| --- | --- |
| Go types and the branch rule | `server/internal/contract/contract.go` |
| Kotlin types and the branch rule | `app/src/main/java/com/team27/mama/contract/Contract.kt` |
| One canonical JSON sample per message | `server/internal/contract/testdata/*.json` |
| Postgres schema | `server/migrations/0001_init.sql` |

Both round-trip tests (`TestRoundTripSamples` in Go, `ContractRoundTripTest`
in Kotlin) read the same sample files, parse each one strictly (an unknown
field fails) and check the re-emitted JSON is the same.

## Conventions

- JSON keys are `snake_case`.
- Timestamps are RFC 3339 strings in UTC, for example
  `2026-10-24T09:10:00.253Z`. On Android, `Instant.now().toString()`. Compare
  them as instants, not as strings: Go drops trailing zeros in the fraction.
- Phone numbers are E.164 (`+58...`).
- Speeds are **meters per second** (Android `Location.getSpeed()`), distances
  and accuracy are meters, coordinates are WGS84 degrees.
- An optional field is left out when it has no value. Never send `null`.
  In particular, when `Location.hasSpeed()` is false, leave `speed_mps` out:
  never send `0`, which would read as "still" and skip the call.
- IDs: `device_id` is a UUID the app makes at install (there is no auth).
  `incident_id` is a UUID the app makes per alert, so a retried alert is the
  same incident.

## The 3 branches by speed

The server picks the branch from the speed of the **newest** point in the
alert. Thresholds live in one place per side: `contract.StillBelowMPS` and
`contract.FastFromMPS` in Go, mirrored by `SpeedThresholds` in Kotlin.

| Branch | `branch` value | Speed (m/s) | Speed (km/h) | What happens |
| --- | --- | --- | --- | --- |
| Still | `immediate_alert` | `< 1.0` | `< 3.6` | The family is alerted at once, no call. |
| Otherwise | `call` | `1.0` to `< 7.0`, or unknown | `3.6` to `< 25.2` | "Mamá" calls. |
| Moving fast (a car) | `silent_tracker` | `>= 7.0` | `>= 25.2` | Silent tracker mode: the phone keeps sending points, no call. |

An unknown speed (the fix has no `speed_mps`, or it is negative) picks
`call`, because the call asks the person (the phone must leave `speed_mps`
out rather than send `0`, see Conventions). `ChooseBranch` (Go) and
`chooseBranch` (Kotlin) implement this table; `TestChooseBranch` and its
Kotlin mirror test both sides of every threshold.

## Messages

Endpoints are listed for the slices that implement them (#17 intake,
#18 call engine, #20 notifier, #55 `GET /config`).

### Point

A GPS fix, used inside other messages. `accuracy_m` and `speed_mps` are
optional.

```json
{"lat": 10.4961, "lng": -66.8531, "accuracy_m": 12.5, "speed_mps": 0.4, "recorded_at": "2026-10-24T09:10:00Z"}
```

### DeviceRegistration (phone → server, `POST /devices`)

Sent at the end of first-run setup and whenever the setup changes; the server
upserts the `devices` row. `app_version`, `ed25519_public_key` and
`ed25519_signature` are optional. Sample: `device_registration.json`.

```json
{
  "device_id": "3f1c9a52-7b1e-4c8a-9a0e-2d6b8f4e1a77",
  "phone_number": "+584141234567",
  "family_numbers": ["+584241112233", "+584125556677"],
  "duress_phrase": "dile a la tía Carmen que llego tarde",
  "app_version": "0.1.0",
  "ed25519_public_key": "Qjj0xMgcOctYzPP1+kmbgiXDnjbvMvg+Cm1vpph2NXo=",
  "ed25519_signature": "JMio38RGA+n2az7/Xiiz4y7724rZcyDt7N90vGav0Vq8SwTXFvJSmH21VJeL323EPmedwMiyrm79YVgIjvXlAQ=="
}
```

`ed25519_public_key` is the device's Ed25519 identity public key (bitchat's,
#56): standard Base64 with padding of exactly 32 bytes. The server derives
`key_id` from it to verify offline payloads (#57). It is optional: a
device with no key bound yet may register without one (and bind one later).
Once a key is bound to a `device_id`, every later registration for it must
carry that same key with a valid proof; a registration with no key or a
different key is answered `409` and changes nothing. A key already registered
to another `device_id` is also `409`; a malformed key is `400`. A phone that
loses its key (reinstall, cleared data) therefore needs a new `device_id`.

`ed25519_signature` proves the phone holds the key's private half: standard
Base64 with padding of the 64-byte Ed25519 signature, by that key, over the
ASCII bytes `MAMA-device-registration-v1` followed by the UTF-8 bytes of
`device_id` (no separator; `RegistrationSignedMessage` in Go,
`DeviceRegistration.signedMessage` in Kotlin). Rules, all checked before any
database access:

- `ed25519_public_key` present: `ed25519_signature` is required and must
  verify, else `400`.
- `ed25519_signature` without `ed25519_public_key`: `400`.
- neither: allowed only while the device has no bound key, else `409`.

The proof covers only `device_id`: it shows the sender holds the key, not that
the other fields (`phone_number`, `family_numbers`, `duress_phrase`,
`app_version`) are the owner's. It gates who may update a bound device; it does
not authenticate the content.

Why: bitchat broadcasts every device's Ed25519 public key on the mesh, so
without the proof anyone in range could register a victim's key to their own
`device_id` first, and the victim's registration would then get `409` and the
server would attribute the victim's offline payloads to the squatter. The
context string keeps this signature from being valid as any other MAMÁ
signature. The sample's signature is real: the payload vector key
(`device_ed25519_seed` in `server/internal/emergency/payload/testdata/vectors.json`)
over the sample `device_id`.

### RemoteConfig (server → phone, `GET /config`)

Public, no auth. The reply is `200` with `Content-Type: application/json`.
Sample: `remote_config.json`.

```json
{
  "sms_number": "+15555550100",
  "server_ed25519_public_key": "/x+fnERHrOfQ9HPdLiQP9ya2eBtqDNBmsrjsP9ZnMBE=",
  "server_x25519_public_key": "14/r5bky56/VN9vYfVVwD79P59iaXXdMRrOvYZxUGj0="
}
```

- `sms_number`: the central number for the SMS fallback, E.164; omitted when
  the server has none.
- `server_ed25519_public_key`: verifies the server's signed acks (std Base64,
  32 bytes, always present).
- `server_x25519_public_key`: the sealed-box key phones encrypt offline
  payloads to (std Base64, 32 bytes, always present).

The phone caches the last good reply and falls back to a build-time default
SMS number.

### AlertEvent (phone → server, `POST /incidents`)

The trigger. `points` has at least one fix, oldest first. `trigger` is one of
`volume_pattern`, `widget`, `tile`, `in_app`. The device must be registered
first (`incidents.device_id` references `devices`): for an unknown
`device_id` the server answers `422`, and the phone re-sends its
DeviceRegistration and then the alert. Sample: `alert_event.json`.

```json
{
  "incident_id": "9b2e4f1d-3c5a-4e7b-8d6f-1a2b3c4d5e6f",
  "device_id": "3f1c9a52-7b1e-4c8a-9a0e-2d6b8f4e1a77",
  "trigger": "volume_pattern",
  "triggered_at": "2026-10-24T09:10:00.253Z",
  "points": [
    {"lat": 10.4961, "lng": -66.8531, "accuracy_m": 12.5, "speed_mps": 0.4, "recorded_at": "2026-10-24T09:10:00Z"}
  ],
  "has_audio": false
}
```

### AlertResponse (server → phone, reply to `POST /incidents`)

The branch decision. `speed_mps` is the speed it was picked from, left out
when unknown. Sending the same `incident_id` again returns the stored
decision, even if the retried points would pick another branch.
Sample: `alert_response.json`.

```json
{"incident_id": "9b2e4f1d-3c5a-4e7b-8d6f-1a2b3c4d5e6f", "branch": "immediate_alert", "speed_mps": 0.4}
```

### LocationUpdate (phone → server, `POST /incidents/{incident_id}/points`)

More fixes for an open incident, mainly in `silent_tracker` mode. A resent
fix (same `recorded_at`) is ignored. Sample: `location_update.json`.

```json
{
  "incident_id": "9b2e4f1d-3c5a-4e7b-8d6f-1a2b3c4d5e6f",
  "device_id": "3f1c9a52-7b1e-4c8a-9a0e-2d6b8f4e1a77",
  "points": [
    {"lat": 10.4975, "lng": -66.8502, "accuracy_m": 8, "speed_mps": 11.2, "recorded_at": "2026-10-24T09:10:15Z"},
    {"lat": 10.4989, "lng": -66.8477, "accuracy_m": 9.5, "recorded_at": "2026-10-24T09:10:30Z"}
  ]
}
```

### CallWebhook (ElevenLabs → server, `POST /webhooks/elevenlabs`)

The ElevenLabs post-call webhook (`type: "post_call_transcription"`). Only
the fields below are modeled; the real payload has more and both sides
ignore them. The call engine passes `incident_id` as a dynamic variable when
it places the call, and the webhook carries it back. `conversation_id` is
unique per call, so a repeated webhook updates the same `calls` row.
`role` is `agent` ("Mamá") or `user`. `dynamic_variables` values are any
JSON: ElevenLabs adds `system__*` variables, some numeric.

How the webhook finds its call: the call engine inserts the `calls` row
before placing the call and stores the `conversation_id` from the
outbound-call response. The webhook handler updates the row with that
`conversation_id`; if none matches yet (the webhook beat that write), it
updates the newest row of the incident whose `conversation_id` is still
NULL; only if there is none does it insert. Sample: `call_webhook.json`.

```json
{
  "type": "post_call_transcription",
  "event_timestamp": 1761297060,
  "data": {
    "agent_id": "agent_01jxample0000000000000000",
    "conversation_id": "conv_01jxample0000000000000000",
    "status": "done",
    "transcript": [
      {"role": "agent", "message": "¿Aló? Mi amor, ¿dónde estás?", "time_in_call_secs": 0},
      {"role": "user", "message": "Sí, mami. Oye, dile a la tía Carmen que llego tarde.", "time_in_call_secs": 4}
    ],
    "metadata": {"start_time_unix_secs": 1761297010, "call_duration_secs": 42},
    "conversation_initiation_client_data": {
      "dynamic_variables": {
        "incident_id": "9b2e4f1d-3c5a-4e7b-8d6f-1a2b3c4d5e6f",
        "system__call_duration_secs": 42,
        "system__called_number": "+584141234567"
      }
    }
  }
}
```

### NotifierMessage (server → notifier)

What the notifier sends to the family: the location and the verdict, and
**no link**. `verdict` is `duress`, `no_duress` or `unclear`; `verdict` and
`reason` are left out when no call happened (`immediate_alert`,
`silent_tracker`). `incident_id` is the dedupe key: one message per incident.
Before sending, the sender claims the incident with
`UPDATE incidents SET notified_at = now() WHERE id = $1 AND notified_at IS NULL`
and sends only if that changed a row (at most once). When an incident has
several calls, the verdict is `duress` if any call says `duress`, otherwise
the newest call's verdict. Sample: `notifier_message.json`.

```json
{
  "incident_id": "9b2e4f1d-3c5a-4e7b-8d6f-1a2b3c4d5e6f",
  "to": ["+584241112233", "+584125556677"],
  "branch": "call",
  "location": {"lat": 10.4961, "lng": -66.8531, "accuracy_m": 12.5, "recorded_at": "2026-10-24T09:10:00Z"},
  "verdict": "duress",
  "reason": "the user said the duress phrase",
  "triggered_at": "2026-10-24T09:10:00.253Z"
}
```

## Postgres schema

`server/migrations/0001_init.sql`; `0002_device_keys.sql` adds
`ed25519_public_key` and `key_id` to `devices`; `0003_emergencies.sql` adds the `emergencies` table. The server applies pending migrations on
every boot (`db.Migrate`, under a Postgres advisory lock), so each Render
deploy migrates the live database with no manual step, and booting again is a
no-op. Without `DATABASE_URL` the server boots and skips migrations.

| Table | Key | Holds |
| --- | --- | --- |
| `devices` | `id` = `device_id` | phone number, family numbers, duress phrase; `ed25519_public_key` (unique, 32 bytes) and `key_id` (unique, 8 bytes: the payload device_id derived from it), both NULL or both set |
| `incidents` | `id` = `incident_id` | device, trigger, branch, speed used, `notified_at` |
| `gps_points` | identity; unique `(incident_id, recorded_at)` | every fix of an incident |
| `calls` | identity; unique `conversation_id` | call status, transcript (JSONB), duration, **verdict** |
| `emergencies` | `emergency_id` (16 hex) | offline emergency: device, status, highest seq, last fix, channels, min hops (#57) |
| `schema_migrations` | `version` | applied migration files |

The verdict lives on `calls` (`verdict`, `verdict_reason`, `verdict_at`)
rather than in its own table: the detector judges one call's transcript, and
an incident can have more than one call when a call is retried. The
`trigger`, `branch`, `status` and `verdict` columns have `CHECK` constraints
with the same values as the Go and Kotlin enums.

Rules for migrations: add a new `NNNN_name.sql` file, never edit a merged one.

## Offline emergency payload

When the phone has no data, it sends a compact signed binary payload by SMS or
over the bitchat mesh. This is not JSON and does not use the endpoints above.
The format, the signature context, the sealed box and the transport texts are
in [`docs/adr/0001-offline-emergency-payload.md`](adr/0001-offline-emergency-payload.md);
the shared test vectors are in
`server/internal/emergency/payload/testdata/vectors.json`.

Its 8-byte `device_id` (the first 8 bytes of SHA-256 of the device Ed25519
public key) is not the UUID `device_id` used above. `devices.key_id` stores
those 8 bytes for every device that registered its key, and
`devicekeys.Lookup` maps a payload's `device_id` to the device row and its
public key.

Endpoints (#57, `server/internal/emergency/ingest/http.go`). Both answer `200`
`text/plain` with the signed ack text `MAMA1:ACK:<std Base64>` (also for a
duplicate), to be rebroadcast verbatim by a relay.

- `POST /emergencies`: body is the SMS text `MAMA1 <Base64url>` of the signed
  payload, any `Content-Type`. Channel `http`.
- `POST /emergencies/relay`: JSON `{"text":"MAMA1:SEAL:<Base64>","hops":2}`;
  `hops` is optional (`0..255`, omitted = unknown). Unknown JSON fields are
  rejected. Channel `mesh`.

Errors are JSON `{"error":"..."}`: `400` malformed payload, text, JSON or
hops; `403` bad signature; `409` the `emergency_id` belongs to another device;
`413` body over 1 KiB; `422` device not registered; `503` no database; `500`
anything else. 4xx is permanent: retry only 5xx.
