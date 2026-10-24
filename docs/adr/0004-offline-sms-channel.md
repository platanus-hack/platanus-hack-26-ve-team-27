# 0004. Offline SMS channel

## Status

Accepted, 2026-10-24. Ticket #58, spec #62 sections 2, 3 and 7.

## Context

With no data but some cell signal (2G or voice only), the phone can still send
a text. SMS is one of the two offline channels of ADR 0001; the other is the
bitchat mesh. The SMS must go out without the user touching anything, because
the person in danger may not be able to. The server must receive it through a
provider that can change, and answer so the phone knows it arrived.

## Decision

1. **Background send with `SmsManager` and `SEND_SMS`.** The app texts the
   central number with `SmsManager.sendTextMessage`, with no UI and no user
   interaction. The number comes from the remote config (`sms_number`, #55), never hardcoded. A blank number or a missing permission
   fails without sending. The sent-status `PendingIntent` (immutable) tells
   the app whether the radio accepted the message, so a failed send is retried.
2. **Sideload only.** Google Play forbids `SEND_SMS` unless the app is the
   default SMS app, and MAMÁ will not be. We accept that: the build that uses
   this channel is distributed by sideload (APK), as the demo already is.
3. **Alternative for a store build** (not built): a prefilled
   `ACTION_SENDTO` intent (`smsto:<number>` with the text as `sms_body`). It
   needs no permission, but it opens the SMS app and the user must tap send,
   so it is not background and not for an emergency the user cannot attend.
4. **One SMS, signed, not encrypted.** The body is `MAMA1 ` + Base64url of the
   98-byte signed payload (ADR 0001): 137 characters, under the 160 of a single
   SMS. The channel checks the length and sends nothing longer. It is not sealed
   because a sealed box does not fit one SMS (that is why the mesh seals and the
   SMS does not); the carrier can read the location. The signature is what
   authenticates it.
5. **Retry policy.** One attempt plus up to three retries, waiting 2 s, 5 s and
   10 s before them (17 s in total). A failed attempt is a sent status other
   than OK, or an exception from `SmsManager`. A missing permission is not
   retried. The send runs in a coroutine that the caller (#59) owns; it can be
   cancelled, and `SmsChannel.stop()` ends the send at once, including a
   backoff in progress. An SMS already handed to the radio cannot be recalled.
6. **Inbound side on the server.** The SMS provider calls
   `POST /sms/inbound/{provider}`. The `InboundSMSProvider` interface verifies
   the webhook signature and normalizes it to `{from, body}`; Twilio is the
   first implementation, and another provider is one more adapter (already
   merged, `docs/contract.md`, "Inbound SMS"). The body enters the same pipeline
   as `POST /emergencies` (ADR 0003) with channel `sms`.
7. **Ack by SMS reply.** The server answers the webhook with TwiML carrying
   the signed ack text `MAMA1 <Base64url>`. The phone listens for that SMS and
   stops sending (a later slice of #58). Until then nothing stops the channel
   but its own retry limit.
8. **Behind the offline flags.** The channel exists only in the offline
   emergency path: the build flag `OFFLINE_EMERGENCY_ENABLED` on the app and
   `EMERGENCY_OFFLINE_ENABLED` on the server (#74). With them off, nothing
   calls it.

## Consequences

- The app cannot be listed on Google Play with this channel. A store build
  would drop `SEND_SMS` and use the intent of point 3 (one tap, in the
  foreground).
- The carrier sees the location and the sender's number. The server number is
  also a public destination: anyone can text it, so only the signature, device
  registration and `emergency_id` binding (ADR 0003) decide what is stored.
- "Sent" means the radio accepted the SMS, not that the server got it. Only the
  ack proves delivery; until the ack listener lands, a success does not stop
  #59 from also using the mesh and HTTP.
- SMS costs money and needs a number and a provider account (Twilio) in every
  deployment; the credentials stay in environment variables.
