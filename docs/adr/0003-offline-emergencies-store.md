# 0003. Offline emergencies store

## Status

Accepted, 2026-10-24. Ticket #57, spec #62 section 8.

## Context

The server receives the same offline emergency (ADR 0001) through up to three
channels and several copies: the app over HTTP, an SMS to the server number,
and any number of mesh relays that reach the internet. The copies arrive in any
order, at the same moment, and repeatedly. The server must keep one record per
emergency, never move it backwards, and ack every copy.

#62 asked for SQLite (with an ADR for the standard-library exception). That
predates the move to Postgres: the server already runs Postgres with pgx and
applies migrations on boot (`0001_init.sql`, `db.Migrate`), so SQLite would add
a second database and a new dependency for nothing.

## Decision

1. **Postgres**, migration `0003_emergencies.sql`, table `emergencies`. No new dependency.
2. **Own table, not `incidents`.** Incidents are UUID-keyed and carry trigger,
   branch and speed CHECKs, call rows and the notifier's `notified_at` claim;
   an offline emergency has none of those, and the incidents schema is frozen.
3. **Key = `emergency_id`** as 16 lowercase hex characters, global: one row
   however many relay, SMS or HTTP copies arrive. A row belongs to the device that
   created it. The same `emergency_id` from another device is a conflict (HTTP
   409) and changes nothing: updates and cancels are accepted only from the
   alert's device, and the store never overwrites a row with another `device_id`
   (the upsert only updates a row with the same `device_id`). `emergency_id` is
   the global primary key rather than `(device_id, emergency_id)`: a receipt for
   an existing id from another `device_id` is rejected with 409, so updates and
   cancels are bound to the alert's device, equivalent to keying by
   `(device_id, emergency_id)` for every legitimate flow. The residual risk,
   another registered device racing a just-intercepted id before the server's
   first receipt, is accepted.
4. **Seq rules.** The store keeps the highest accepted `seq`. Location and
   `sent_at` only advance with a higher seq, and a location only with a fix (a
   payload without a fix keeps the last one). A cancel closes the emergency at
   any seq and is terminal. A replay or stale copy gets an ack carrying the
   stored highest seq.
5. **Channels and hops.** `channels` is the set of channels that delivered a
   copy, in first-seen order; `min_hops` is the smallest known mesh hop count
   (NULL if unknown or never via mesh).
6. **Concurrency.** Each update runs in one transaction under
   `pg_advisory_xact_lock(emergency_id)`, so concurrent copies of one id merge
   one at a time, including the first insert, and publish one event. Any future
   writer of `emergencies` (for example the Kapso module setting `acked`) must
   take the same `pg_advisory_xact_lock`, or its update can be lost to a
   concurrent ingest upsert.
7. **`acked`** is reserved for a family contact's acknowledgment (a later
   ticket). #57 never sets it; its only transition is any status to `cancelled`.

## Consequences

- No per-delivery log: the panel sees the aggregate (channels, min hops, last receipt).
- Offline emergencies do not go through the incidents notifier. The Kapso
  module consumes `EmergencyReceived` events instead.
- Time columns are the server clock, except `sent_at` and `location_sent_at`,
  which are the phone clock (the payload timestamp).
- Another device that learns an `emergency_id` cannot alter or cancel it; it only gets a 409.
- Events are published from a goroutine after the commit: for one emergency they
  may arrive out of order, and at most once (lost on a crash or redeploy;
  duplicate receipts are not re-published). Consumers must treat `cancelled` as
  terminal (never `cancelled` to `open`), and the Kapso consumer needs its own
  durable claim (for example a `notified_at` column, like incidents). Not built
  in #57.
