# 0005. Incident report seal

## Status

Accepted, 2026-10-24. Ticket #76.

## Context

The incident report (`GET /i/{token}`) is what a family takes to the CICPC or
the prosecutor: the timeline, the locations, the transcript, the verdict and
the call recording. They need to show later that the record they bring is the
one the server held when the call ended, and anyone should be able to check
that without trusting the page that shows it.

The server already has an Ed25519 key, `EMERGENCY_ED25519_SEED` (#55). It signs
offline acks with the context `MAMA-offline-emergency-v1` (ADR 0001), and
phones pin its public half from `GET /config`.

## Decision

1. **What is signed.** `ficha.json`: one JSON document per incident built by
   `report.Document`. It holds the incident (trigger, branch, speed, phone and
   server times, `notified_at`, how many family numbers), every GPS point,
   every call (status, times, duration, transcript, verdict and reason, and
   the recording's SHA-256, size, type and storage time), `sealed_at`, the
   server public key and a `clocks` note saying which clock each time comes
   from. It leaves out every phone number, the device id and the duress
   phrase (the phrase appears only where the person said it in the
   transcript). The device id is the device's only credential on the
   unauthenticated `POST /devices` and `POST /incidents/{incident_id}/points`,
   and anyone holding the link downloads the file: with it they could
   re-register the device to their own numbers or add points to its
   incidents. The audio is covered by its SHA-256, not embedded.
2. **Exact bytes.** The document is stored as it was signed
   (`incident_seals.document`, `BYTEA`) and served byte for byte as
   `GET /i/{token}/ficha.json`. There is no canonicalization step to agree on:
   the verifier checks the bytes it has. Changing any byte, whitespace
   included, breaks the seal.
3. **Signature.** Pure Ed25519 (RFC 8032, no prehash) by the server key over
   `MAMA-incident-report-v1 || document`. The 23-byte ASCII context is not in
   the file. With it, a report signature is never a valid offline ack
   (`MAMA-offline-emergency-v1 || ...`) or any other MAMÁ signature, and none
   of those is a valid report seal, though one key signs them all. This is the
   same argument as ADR 0001. `ficha.sig` (`GET /i/{token}/ficha.sig`) is the
   64-byte signature in standard Base64 plus a newline; the verifier also
   accepts the 64 raw bytes.
4. **Key.** The same key as `GET /config` (`server_ed25519_public_key`): the
   offline channel's when `EMERGENCY_OFFLINE_ENABLED` is on, otherwise
   `EMERGENCY_ED25519_SEED` alone. The report never generates a key, so
   Render never seals with an ephemeral one. Without a seed the report still
   works and says "Sin sellar"; the server logs a warning and boots.
5. **When.** The call engine's post-call hooks run in order after the
   transcript is stored (`call.Hooks` in `main.go`). The report's hook runs
   last and works in the background:
   1. It fetches the recording from ElevenLabs. While `has_audio` is false it
      retries for about 2 minutes, then gives up. It stores the bytes and
      their SHA-256 in `call_recordings`; Render's disk is ephemeral.
   2. It waits until the call has a verdict and the incident has
      `notified_at`, polling for at most 60 s. #21 adds the detector and the
      notifier earlier in the same hook list, so the seal normally includes
      both. Until #21 lands it waits the full 60 s and seals without them.
   3. It seals once, and only when no call of the incident is still
      requested or in progress and this call is its newest finished one
      (see 6). `INSERT ... ON CONFLICT DO NOTHING` keeps the first seal, and
      a repeated webhook changes nothing.

   If a restart kills that goroutine, or the retry failed after it declined
   to seal, the first view of the page more than 5 minutes after the newest
   finished call ended, with no call still in progress, starts it again
   (`Closer.Recover`), once per call per process.
6. **Scope.** Only `call`-branch incidents are sealed. `immediate_alert` and
   `silent_tracker` reports render but say "Sin sellar" (out of scope in #76).
   A failed call is not sealed on its own, and neither is a call ElevenLabs
   reports done while the retry rings or runs (a phone that rang out can
   still get a post-call webhook): the retry's webhook seals, and when the
   retry fails too, `Closer.Recover` seals with the newest finished call.
   A call still requested or in progress 15 minutes after it was requested
   no longer counts: the server lost it (it died between placing the call
   and its outcome), and it must not keep the report unsealed for good.
7. **The PDF.** `GET /i/{token}/reporte.pdf` reads a sealed report back from
   the stored `ficha.json`, never from the live rows, so it states exactly
   what the seal covers. It adds what the seal leaves out (each family
   message's delivery status, linked offline emergencies) under a note that
   says so. Before the seal it is a draft, marked "Borrador, sin sellar".
8. **Verification.** `go run ./cmd/verify-ficha ficha.json ficha.sig <key>`
   prints "firma válida" or why not. The key must come from the page or from
   `GET /config`, never from `ficha.json` itself. The document names the key,
   and the verifier rejects a valid signature whose document names another
   key.

## Consequences

- The page shows the live record and the seal covers `ficha.json` as it was
  at `sealed_at`. Points or calls stored after the seal appear on the page
  but not in the file. The page lists recording hashes from the sealed bytes,
  never from rows added later. A recording is never added after the seal.
- What the seal proves, and the page says so: the record did not change since
  the MAMÁ server closed it. It is not a certified electronic signature and
  does not replace a forensic examination. It cannot prove that the phone's
  clock or the transcript is right, only that neither changed afterwards.
- If the server key leaks or rotates, old seals still verify against the old
  public key. Keep a record of each key's fingerprint and the dates it was
  used. Revocation is out of scope.
- The link token is stored in plain text: anyone who can read the database
  already reads the reports themselves.
- The live page (SSE) shows more than the seal covers: Twilio's ringing and
  answer times (`call_progress`), the derived WebRTC fallback, offline
  emergencies matched by device and time, and the latency numbers. The
  document format stays v1 and leaves them out, because they are either
  derived or linked by a heuristic, not recorded facts about this incident.
