# 0004. Background SMS, sideload only

## Status

Accepted, 2026-10-24. Ticket #58, spec #62 sections 2, 7 and 10.

## Context

When the phone has cellular signal but no data, the offline emergency (ADR
0001) goes out as one SMS to the central number from the remote config (#55):
`MAMA1 ` plus Base64url of the 98-byte signed payload, 137 characters. The
person declaring the emergency may not be able to touch the phone again, so the
SMS has to leave without any interaction, be retried up to three times with
backoff, and stop when the server's ack SMS arrives.

Android only lets an app send an SMS silently through `SmsManager` with the
`SEND_SMS` permission. Google Play restricts `SEND_SMS` and `RECEIVE_SMS` to the
default SMS app and a few approved exceptions; an emergency app that is not the
default SMS handler does not qualify.

The server side receives the SMS through a provider webhook. Twilio is the
provider we already use for the call and the SMS fallback, but the domain must
not depend on it.

## Decision

1. **Send in the background with `SmsManager.sendTextMessage`** under
   `SEND_SMS`: one part, never multipart, no user tap. One attempt plus up to
   three retries with backoff; a failed send counts as an attempt.
2. **Listen for the ack with `RECEIVE_SMS`.** A manifest receiver for
   `SMS_RECEIVED` (guarded by `BROADCAST_SMS`) accepts only an SMS from the
   central number whose body is a `MAMA1 ` ack that verifies with the server
   Ed25519 key from the remote config, for the emergency being sent and at
   least its seq. Anything else is ignored. The sender address can be spoofed,
   so the check that matters is the ack signature; the number check only
   drops noise.
3. **Distribution is sideload only.** We accept that the app cannot be on Play
   with this channel. The APK is installed from the release link.
4. **Everything is behind the offline emergency flags**: the app build flag
   `mama.offlineEmergency` (the receiver is disabled and nothing sends when it is
   off) and the server's `EMERGENCY_OFFLINE_ENABLED`.
5. **Server: `InboundSMSProvider`.** Each provider adapter validates its webhook
   signature and normalizes it to `{from, body}`; the domain handler at
   `POST /sms/inbound/{provider}` feeds the body to the #57 ingest pipeline as
   channel `sms` and replies with the ack SMS (`MAMA1 ` plus Base64url of the
   76-byte ack, 108 characters) through the same provider. Twilio is the first
   adapter: `X-Twilio-Signature` checked against `TWILIO_AUTH_TOKEN` over the
   public URL built from `TWILIO_WEBHOOK_BASE_URL` (never from the request's
   Host), and the ack goes back as TwiML in the webhook response, so no
   outbound API call is needed. Without both variables the provider is not
   mounted; an unsigned webhook is never accepted. A non-MAMA or invalid SMS
   gets no reply.

## Alternative for a Play release

Replace the silent send with an `ACTION_SENDTO` intent to `smsto:<central
number>` with the body prefilled (`sms_body`). It needs no SMS permission and is
allowed on Play, but the user must tap Send in the SMS app, once per attempt, so
there are no unattended retries. The ack can still be noticed without
`RECEIVE_SMS` by checking the server over HTTP or mesh when one comes back, or
with the SMS Retriever / User Consent APIs, which need a tap too. Switching is a
change to the app's `SmsSender` only; the server does not change.

## Consequences

- The app cannot be published on Play as is. The sideload path (and Android's
  "unknown sources" prompt) is part of installing MAMÁ.
- The SMS is readable by the carrier and the provider: it is signed, not
  encrypted (ADR 0001).
- The phone pays for up to four SMS per emergency update, and the ack SMS is
  billed to the Twilio number.
- Changing SMS provider means adding one adapter that implements
  `InboundSMSProvider` and registering it under its path name; the domain and
  the app stay the same.
- The retries and the ack wait run on an in-process timer. #59 must emit from
  a foreground service, or Doze and process death delay or drop them.
- `TWILIO_WEBHOOK_BASE_URL` must match the URL set in the Twilio console byte
  for byte (no default `:443`), or every webhook fails its signature.
- A random SMS to the central number is dropped silently, so the number does
  not reveal anything to a stranger.
