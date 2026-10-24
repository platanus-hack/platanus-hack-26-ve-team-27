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

## The branch: by trigger

The server picks the branch from **how the alert was started**, not from the
speed (product decision, 24 Oct; it replaces the speed rule and the old
`CALL_WHEN_STILL` switch):

| Trigger | `branch` value | What happens |
| --- | --- | --- |
| `widget` (home-screen widget) | `immediate_alert` | The family is alerted at once, no call. |
| `volume_pattern`, `tile`, `in_app` | `call` | "Mamá" always calls, whether the phone is still, walking, in a car or reports no speed. |

`silent_tracker` stays a valid `branch` value in the shape (and in stored
incidents), but the server no longer picks it. The response still carries
`speed_mps` from the newest point, and every point is stored. The rule lives
only on the server (`chooseBranch` in `server/internal/intake/branch.go`,
tested by `TestChooseBranchByTrigger`); the phone follows the `branch` in the
response. `ChooseBranch` (Go) and `chooseBranch` (Kotlin), the old speed
table with `contract.StillBelowMPS` and `contract.FastFromMPS`, remain in the
contract code but no longer decide the branch.

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
upserts the `devices` row. `app_version`, `user_name`, `safe_phrase`,
`ed25519_public_key` and `ed25519_signature` are optional. Sample:
`device_registration.json`.

```json
{
  "device_id": "3f1c9a52-7b1e-4c8a-9a0e-2d6b8f4e1a77",
  "phone_number": "+584141234567",
  "family_numbers": ["+584241112233", "+584125556677"],
  "duress_phrase": "dile a la tía Carmen que llego tarde",
  "app_version": "0.1.0",
  "user_name": "Mariana",
  "safe_phrase": "ya llegué a la casa, todo tranquilo",
  "ed25519_public_key": "Qjj0xMgcOctYzPP1+kmbgiXDnjbvMvg+Cm1vpph2NXo=",
  "ed25519_signature": "JMio38RGA+n2az7/Xiiz4y7724rZcyDt7N90vGav0Vq8SwTXFvJSmH21VJeL323EPmedwMiyrm79YVgIjvXlAQ=="
}
```

`user_name` is how "Mamá" greets the person and `safe_phrase` is the "todo
bien" phrase the person says when they are safe (#104). Both are optional so
older apps keep working: a registration without one (or with only spaces)
keeps the stored value, and the server trims both. Rules, answered `422`
with an `error` that never quotes a phrase:

- `user_name`: 1 to 30 characters.
- `duress_phrase` and `safe_phrase`: at most 150 characters and 20 words each,
  where a word is a run of letters or digits. Both are said aloud in a call
  that has to sound ordinary, and the caps keep the duress detector fast: its
  cost grows with the cube of the words when it compares the two phrases, on
  a route with no auth.
- `safe_phrase`: at least one word; different from `duress_phrase`; and not
  close to it. The server runs the duress detector's rules
  (`server/internal/duress`, same normalization: case, accents, stopwords,
  one-letter typos) on the safe phrase as if the person said it: when the
  verdict is anything but `no_duress` (the phrase, a close variant, or half
  or more of its key words), it is too close.

The phrases are never written to the server logs.

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

### Device voice (phone → server, `/devices/{id}/voice`, #107)

The phone gives a device its own "Mamá" voice from an audio file of the
mother's voice. The server makes it an ElevenLabs instant voice clone named
`mama-<first 8 characters of device_id>` and stores its id in
`devices.voice_id`; from then on every call of that device speaks with it.
Code: `server/internal/voice`. Every route answers `503` without a database
or without `ELEVENLABS_API_KEY`, and `404` for an unknown or malformed
`device_id`. Errors are JSON `{"error":"..."}`.

- `POST /devices/{id}/voice`, `multipart/form-data`, signed (below):
  - `audio`: the file, mp3, wav, m4a, ogg or webm (told by its first bytes,
    not its name), at most 10 MB.
  - `consent`: exactly `true`: the voice's owner agreed to the clone.
    Missing or anything else is `400`.

  `201 {"voice_id":"..."}` on success. `400` bad form, no consent or another
  format; `413` over 10 MB; `429` with `Retry-After` when the device
  uploaded less than a minute ago; `502` when ElevenLabs fails, and then the
  device keeps its previous voice and may retry at once; `503` with
  `Retry-After` when 4 uploads are already in progress. An unknown device,
  a device with no key or a stale `X-Mama-Signed-At` are refused before the
  body is read. The new voice is stored first and the
  old one is deleted from ElevenLabs after. The server stores
  `voice_consent_at` and `voice_consent_sha256` (SHA-256 of the audio); the
  audio itself is read in memory and never written to disk or to the
  database.
- `DELETE /devices/{id}/voice`, signed: deletes the clone in ElevenLabs and
  clears `voice_id`, `voice_consent_at` and `voice_consent_sha256`, so calls
  go back to the global voice. `204`, also when there was no voice; `502`
  when ElevenLabs fails, and then nothing changes.
- `GET /devices/{id}/voice/preview`: `200 audio/mpeg`, "¡Hola, mi amor!
  ¿Cómo vas?" said by the device's voice with the agent's TTS model and
  settings (`call/agent/tts.json`), for the setup screen to play. Not
  signed; cached per voice, so playing it again costs no credits. `404` when
  the device has no voice of its own; `502` when ElevenLabs fails.

Signature. Like `POST /devices`, it proves the phone holds the device's
bound Ed25519 key (the key from the registration, never one sent with this
request). Two headers:

- `X-Mama-Signed-At`: the phone's clock, unix seconds. It must be within
  5 minutes of the server's.
- `X-Mama-Signature`: standard Base64 with padding of the 64-byte Ed25519
  signature over the ASCII bytes

  ```
  MAMA-device-voice-v1 <METHOD> <device_id> <signed_at>[ <sha256>]
  ```

  single spaces, `METHOD` is `POST` or `DELETE`, and `<sha256>` (lowercase
  hex of the SHA-256 of the `audio` bytes) only for `POST`. Example:
  `MAMA-device-voice-v1 DELETE 3f1c9a52-7b1e-4c8a-9a0e-2d6b8f4e1a77 1761307800`.

A device with no bound key gets `409` (register a key first); a missing,
expired or bad signature gets `401`. The hash binds the signature to the
file, `signed_at` limits a replay to 5 minutes, and inside that window the
server accepts each signature once (in memory, per server process), so a
new request needs a new `signed_at`. `SignedMessage` in
`server/internal/voice` builds the message.

Fallback for the demo: `go run ./cmd/setvoice --device <id> --file mama.mp3`
does the same straight against the database and ElevenLabs (`--delete`
removes the voice). `go run ./cmd/voiceprep` builds the sample and
`go run ./cmd/voicelab` compares TTS settings (`server/internal/voice` and
the commands' doc comments).

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

The trigger. `points` has 1 to 20 fixes, oldest first (the server answers
`400` to more, and to a body over 16 KiB). `trigger` is one of
`volume_pattern`, `widget`, `tile`, `in_app`. The device must be registered
first (`incidents.device_id` references `devices`): for an unknown
`device_id` the server answers `422`, and the phone re-sends its
DeviceRegistration and then the alert. Sample: `alert_event.json`.

An offline emergency (`POST /emergencies`, the inbound SMS webhook or
`POST /emergencies/relay`) also opens an incident, server-side only: its id is
derived from the `emergency_id`, `trigger` is `offline` (migration 0008; a phone
never sends it), the branch is `immediate_alert`, and each frame with a fix adds
a GPS point. An emergency already cancelled opens nothing.

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

More fixes for an open incident, mainly in `silent_tracker` mode, 1 to 20
per request like the alert's. A resent fix (same `recorded_at`) is ignored.
Sample: `location_update.json`.

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

Dynamic variables: every call "Mamá" makes, the phone call and the WebRTC
fallback page alike, carries all four below, never one missing, because
ElevenLabs refuses a conversation whose prompt names a variable it was not
given. The constants are `contract.DynamicVar*` in Go and `DYNAMIC_VAR_*` in
Kotlin.

| Variable | Value |
| --- | --- |
| `incident_id` | the incident (blank on the fallback page while several are open) |
| `nombre` | `devices.user_name`, or `mi amor` when unset |
| `frase_alerta` | `devices.duress_phrase` |
| `frase_segura` | `devices.safe_phrase`, or `""` when unset |

The fallback page (`GET /call/fallback`) puts these in its HTML while an
incident is open there (up to 15 minutes). With `FALLBACK_TOKEN` set it
answers `401` unless the request has `?token=<FALLBACK_TOKEN>` (compared in
constant time), and sends the phrases. Without it the page stays open but
sends `frase_alerta` and `frase_segura` empty, so a guessed URL never shows
them.

The call speaks with `devices.voice_id` when set, otherwise with the global
`ELEVENLABS_VOICE_ID`, otherwise with the agent's own voice. A device gets
`voice_id` from an audio file (see Device voice).

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

What the notifier sends to the family: the location and the verdict. Its only
link is the incident report's (#76): with `INCIDENT_REPORT_ENABLED` on and
`PUBLIC_URL` set, the text ends with a `Ficha: <url>` line, sent with
`preview_url: false` so WhatsApp fetches no preview. The link is added to the
text, not to this JSON shape. `verdict` is `duress`, `no_duress` or
`unclear`; `verdict` and `reason` are left out when no call happened (`immediate_alert`,
`silent_tracker`). `incident_id` is the dedupe key: one message per incident.
Before sending, the sender claims the incident with
`UPDATE incidents SET notified_at = now() WHERE id = $1 AND notified_at IS NULL`
and sends only if that changed a row (at most once). When an incident has
several calls, the verdict is `duress` if any call says `duress`, otherwise
the newest call's verdict. Sample: `notifier_message.json`.

`reporter` is the reporting device's own `phone_number` (left out when
unknown). With `NOTIFY_SELF=true` (#102, off by default) the notifier adds it
to the recipients on the `NOTIFIER` channel (WhatsApp), once even when it is
also a family number. It is for the demo only: in production the aggressor may
be looking at that phone. Without it the notifier sends one message per family
number, as before. The claim covers every recipient: it is released only when
none got the alert.

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
`ed25519_public_key` and `key_id` to `devices`; `0003_emergencies.sql` adds the `emergencies` table;
`0004_incident_reports.sql` adds `incident_reports`, `call_recordings`,
`incident_seals` and `call_progress` for the incident report (#76, approved
by the Tech lead on Oct 24); `0005_device_setup.sql` adds `user_name`,
`safe_phrase` and `voice_id` to `devices` (#104, approved by the Tech lead on
Oct 24); `0006_notify_deliveries.sql` adds `notify_deliveries` (#76); `0007_device_voice_consent.sql` adds
`voice_consent_at` and `voice_consent_sha256` to `devices` (#107);
`0008_offline_incidents.sql` adds `offline` to the `incidents.trigger` check (#59). The
server applies pending migrations on
every boot (`db.Migrate`, under a Postgres advisory lock), so each Render
deploy migrates the live database with no manual step, and booting again is a
no-op. Without `DATABASE_URL` the server boots and skips migrations.

| Table | Key | Holds |
| --- | --- | --- |
| `devices` | `id` = `device_id` | phone number, family numbers, duress phrase; `user_name`, `safe_phrase` and `voice_id` (the cloned voice, #107), NULL until set; `voice_consent_at` and `voice_consent_sha256` (when the owner consented and the SHA-256 of the audio, never the audio), set with `voice_id`; `ed25519_public_key` (unique, 32 bytes) and `key_id` (unique, 8 bytes: the payload device_id derived from it), both NULL or both set |
| `incidents` | `id` = `incident_id` | device, trigger, branch, speed used, `notified_at` |
| `gps_points` | identity; unique `(incident_id, recorded_at)` | every fix of an incident |
| `calls` | identity; unique `conversation_id` | call status, transcript (JSONB), duration, **verdict** |
| `emergencies` | `emergency_id` (16 hex) | offline emergency: device, status, highest seq, last fix, channels, min hops (#57) |
| `incident_reports` | `incident_id` | the report link: `token` (unique, 22 base64url chars), `created_at`, `expires_at` (#76) |
| `call_recordings` | `call_id` | a call's recording: `content_type`, `audio` (bytes), `sha256`, `stored_at` (#76) |
| `incident_seals` | `incident_id` | the seal: `sealed_at`, `document` (exact bytes of `ficha.json`), `signature`, `public_key` (#76) |
| `call_progress` | `call_id` | what the call engine saw polling Twilio: last `twilio_status`, `ringing_at`, `answered_at` (server clock, #76) |
| `notify_deliveries` | identity; unique `message_id` | each family message the notifier sent: masked `recipient`, `channel`, provider `message_id` (Kapso wamid), `status` (`accepted`, `failed`, `sent`, `delivered`, `read`), `error`, `sent_at`, `delivered_at`, `read_at` (#76) |
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

The offline emergency routes (`GET /config`, `POST /emergencies`,
`POST /emergencies/relay`) exist only when the server runs with
`EMERGENCY_OFFLINE_ENABLED=true`; otherwise they answer `404`. It is off by
default, so the skeleton and the demo path do not depend on them.

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

## Incident report (#76)

A private page per incident for the family: the timeline, the locations, the
call transcript and recording, the verdict and the server's seal. It exists
only when the server runs with `INCIDENT_REPORT_ENABLED=true` (off by
default; then every route below answers `404`). Code:
`server/internal/report`.

- **Link.** `GET /i/{token}`. `token` is 128 random bits in base64url without
  padding (22 characters), made by the server and unrelated to the
  `incident_id` the phone made. One link per incident
  (`incident_reports`); it lives `INCIDENT_REPORT_TTL_DAYS` days (default 30)
  and the next request for a link after it expired gets a new token. An
  unknown, malformed or expired token answers `404`, all alike. The absolute
  URL uses `PUBLIC_URL` (else `RENDER_EXTERNAL_URL`); the family message's
  `Ficha: <url>` line creates the link (see NotifierMessage).
  `go run ./cmd/ficha <incident_id>` prints the link of an incident, creating it if needed.
- **Privacy.** Every response under `/i/` carries `Referrer-Policy:
  no-referrer`, `X-Robots-Tag: noindex, nofollow, noarchive`, `Cache-Control:
  no-store` and a `Content-Security-Policy` that allows only this server.
  Fonts (Lexend, OFL), images, CSS and JS are embedded in the binary and
  served from `/ficha/static/{file}`, so opening a report makes no request to
  anyone else. There is no map with tiles: each location has an "Abrir en
  Maps" link and the path is drawn in SVG. The page never shows the full phone
  number (only `+58 ···67`), the device id or the duress phrase. No other
  response under `/i/` (`ficha.json`, `estado.json`, `/live`, the PDF)
  carries the device id either: it is the device's only credential on
  `POST /devices` and `POST /incidents/{incident_id}/points`.
- **Recording.** The server's post-call hooks are a list in `main.go`
  (`call.Hooks`), run in order after the webhook stored the transcript. The
  report's hook takes a `done` call and, in the background, asks ElevenLabs
  for its audio with `ELEVENLABS_API_KEY` (`GET
  /v1/convai/conversations/{id}`, then `.../audio` once `has_audio` is true),
  retrying for about 2 minutes. It stores the bytes and their SHA-256 in
  `call_recordings` once per call. `GET /i/{token}/audio.mp3?llamada=N`
  serves it from this server (never a link to ElevenLabs), with Range
  support; `N` is the call's 1-based number in request order, and without it
  the newest recorded call. No recording: `404`, and the page says "Audio no
  disponible" and keeps the transcript.
- **Seal.** After the recording, and after waiting up to 60 s for the
  call's verdict and `notified_at`, the server seals a `call` report once,
  for its newest finished call and only when no call of it is still
  requested or in progress (after a failed retry, the first page view 5 min
  after the finished call ended seals it):
  pure Ed25519 by the `GET /config` key over `MAMA-incident-report-v1 ||
  ficha.json`. `GET /i/{token}/ficha.json` serves the exact signed bytes and
  `GET /i/{token}/ficha.sig` the signature in Base64 (both `404` until
  sealed). `go run ./cmd/verify-ficha ficha.json ficha.sig <key>` checks
  them. Without `EMERGENCY_ED25519_SEED` (or the offline channel's key)
  reports stay "Sin sellar" and the server still boots. Format, timing and
  limits: [`docs/adr/0005-incident-report-seal.md`](adr/0005-incident-report-seal.md).
- **The page.** It is mobile first, because the family opens it from
  WhatsApp. From 1024 px wide it fits one screen with no page scroll (the
  projector):
  - a header with the state and the clock since the gesture;
  - a horizontal track of the case's stages (Pidió ayuda → Llegó al servidor →
    Mamá llamó → Sonó → Contestó → Colgó → Veredicto → Familia avisada →
    Sellado);
  - three panels: the route, the call (the player and the transcript, once it
    ends) and the family (one chip per relative with what Kapso reported).
  - `devices.user_name`, when the column exists (#104), names the person.
- **PDF.** `GET /i/{token}/reporte.pdf` is the detailed report for the
  authorities. Once sealed, it is built from the stored `ficha.json`; before
  that it is marked "Borrador, sin sellar". The family messages and linked
  offline emergencies come from the live record and are labelled as outside
  the seal. It uses `github.com/go-pdf/fpdf` with Lexend embedded.
- **Family messages.** With the report on, the notifier records every send in
  `notify_deliveries`, with the number masked. Kapso's message status webhook
  (`whatsapp.message.sent`, `.delivered`, `.read`, `.failed`) goes to
  `POST /webhooks/kapso`:
  - It is signed in `X-Webhook-Signature` with the hex HMAC-SHA256 of the raw
    body and `KAPSO_WEBHOOK_SECRET`. A bad signature answers `401`; without
    the secret the route is not mounted.
  - Statuses only move forward, and a failure never overrides "read".
  - Other events and unknown message ids answer `200`.
- **Live.** `GET /i/{token}/live` is a Server-Sent Events stream. It sends
  an `update` event at once and then one each time the case changes; the
  server re-reads the case every second. The event data is JSON:
  `version`, `state`, `live`, `end`, `server_now`, and `sections`, the page's
  sections rendered as HTML, so the page has a single renderer. A
  `: keep-alive` comment goes out every 15 s. An `end` event follows the seal
  or the moment the case stops being live (nothing stored for 30 min, which
  is how a report that is never sealed ends), after that last state; or
  `{"gone":true}` when the link stops working. Then the stream closes, and
  the polling fallback stops on the same rule. A live stream also closes
  after 30 min, and the browser reconnects.
  `GET /i/{token}/estado.json` is the same snapshot, for polling when the
  stream fails. The family page streams `report.FamilySections` (`status`,
  `header`, `track`, `call`, `route`, `family`). Other pages, such as the
  central dashboard (#99), call `report.RenderSections` and
  `report.Handler.Stream` with their own loader and any sections. They can
  also use `report.BlockSections` (`timeline`, `points`, `latency`, `calls`,
  `emergencies`, `seal`), which the family page does not show
  (`templates/blocks.html` says where each goes). "En vivo", the clock since the gesture and the call timer run
  while the case is unsealed and something happened in the last 30 min.
- **What the page adds beyond the seal.** These appear on the page but are
  not in `ficha.json`:
  - Twilio's ringing and answer times (`call_progress`). The engine's
    optional `Progress` hook stores them while the report is on.
  - Retries (each `calls` row).
  - The WebRTC fallback, derived: no call connected, and the last one failed
    after the retry or on a non-+58 number.
  - Offline emergencies (`emergencies`) of the same device whose
    `[first_received_at, updated_at]` overlaps the incident's span ±10 min.
    The schema has no link between the two, so they are matched by phone
    and time and labelled so.
  - Big latency numbers from the gesture (phone clock) to the server, to
    "Mamá llamando", to the phone ringing (when Twilio was polled) and to
    the WhatsApp claim.
- **Demo.** With `DEMO_KEY` set, `GET /demo/latest?key=K` redirects to the
  newest incident's report (creating its link). `GET /demo/next?key=K` waits
  for the next incident, using the database's clock, and opens its report
  when it arrives. The key is compared in constant time; a wrong or missing
  key answers `404`.
- **Central dashboard (#99).** With `CENTRAL_KEY` set (and the report on),
  `GET /central/` lists every incident live, newest first: name
  (`devices.user_name`), masked number, time, last location and state
  (`activo`, `en llamada`, `alerta` for a `duress` verdict, `cancelado` for
  an explicit cancellation or a `no_duress` verdict that notified nobody,
  `cerrado`). `GET /central/r/{incident_id}` follows one case: the stage
  track, the call with the transcript as it is stored (also during the call),
  the route and points, the timeline, `calls.ficha` (#106) when the column
  exists, and the notified people per channel. Both stream through the same
  live loop as `/i/{token}/live` (`/central/live`,
  `/central/r/{incident_id}/live`, and their `estado.json`). Access:
  `?key=<CENTRAL_KEY>` once, compared in constant time, sets an HttpOnly,
  Secure, SameSite=Strict cookie (an HMAC of the key, 24 h) and redirects to
  the URL without the key. No key, a wrong key or a bad cookie answers `404`,
  the same as with `CENTRAL_KEY` unset. `?tema=claro` starts in light (dark is
  the default); `?seguir=1` on the list opens the next new case by itself.
  Opening a case creates its report link if it has none, for the recording
  and the PDF.
- **Clocks.** Each timeline row says which clock its time comes from: the
  phone (`triggered_at`, `gps_points.recorded_at`), the server
  (`incidents.created_at`, `calls.requested_at`, `verdict_at`, `notified_at`,
  recording and seal times) or ElevenLabs (the call's start, turns and end,
  from the post-call webhook). `notified_at` is claimed before sending, so the
  page says "enviado", never "recibido".
