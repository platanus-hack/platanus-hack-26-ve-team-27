# 0002. Bitchat BLE mesh as module `:mesh`

Status: Accepted

## Context

MAMÁ needs an offline Bluetooth Low Energy mesh so an emergency can leave a phone with no data or SMS coverage. Building a mesh from scratch (discovery, GATT, fragmentation, relay, deduplication, gossip sync, power management) is out of reach in a hackathon.

## Decision

- Copy the bitchat-android core at commit `dfc67dad41691128bc6e7425bd69f2f409cb5bbf` into the Gradle module `:mesh`, package `com.team27.mama.mesh`.
- Relicense the whole repository as GPL-3.0, because the copied code is GPL-3.0 (see `/LICENSE` and `mesh/NOTICE.md`).
- Keep the service UUID, the binary format, the signing scheme and the parameters (TTL 7, 100 cached messages, 12 h cache, PowerManager duty cycle) unchanged, so bitchat iOS and Android phones relay our traffic.
- Send emergencies as a public `MESSAGE` whose text starts with the `MAMA1:` prefix. We add no new message types.
- Use one Ed25519 identity per device (bitchat's signing key).

## Consequences

- We carry GPL obligations: source availability and license notices for the whole repository.
- Any bitchat user can see that a `MAMA1:` message exists. The content is sealed (#54).
- Our node appears in bitchat peer lists under its 16-hex peer ID.
- Receivers drop, and do not relay, a MESSAGE until they hold our ANNOUNCE, so we send an announce before each broadcast.
- A clock skewed by more than 10 minutes breaks announce verification on bitchat receivers. We document the limit instead of working around it.
- ANNOUNCE-before-MESSAGE ordering is guaranteed on the first hop only. Peers two or more hops away may drop the first MESSAGE until gossip sync or periodic announces catch up, so emitters must re-emit until acked.
- Upstream broadcast store-and-forward actually goes through gossip sync (`StoreForwardManager` skips broadcasts), so we copy both.
- The foreground service is deferred to #60.
- We do not track upstream automatically. Re-syncing means copying the same upstream files again at a new SHA (the import commit `daa27db` lists them; `mesh/NOTICE.md` explains the kinds of file) and re-applying the changes recorded in each file header.
