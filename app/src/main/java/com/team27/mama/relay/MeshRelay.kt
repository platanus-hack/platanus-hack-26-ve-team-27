package com.team27.mama.relay

import com.team27.mama.emergency.payload.MamaText
import com.team27.mama.emergency.payload.MeshFrame
import com.team27.mama.mesh.MeshBroadcast
import com.team27.mama.onboarding.NetworkStatus
import java.util.concurrent.atomic.AtomicBoolean
import kotlin.coroutines.cancellation.CancellationException
import kotlinx.coroutines.CoroutineScope
import kotlinx.coroutines.CoroutineStart
import kotlinx.coroutines.channels.Channel
import kotlinx.coroutines.currentCoroutineContext
import kotlinx.coroutines.ensureActive
import kotlinx.coroutines.flow.MutableStateFlow
import kotlinx.coroutines.flow.StateFlow
import kotlinx.coroutines.flow.asStateFlow
import kotlinx.coroutines.flow.catch
import kotlinx.coroutines.launch
import kotlinx.coroutines.withTimeoutOrNull

/**
 * Relays other phones' offline emergencies (#60): a SEAL text heard over the mesh is
 * queued, uploaded verbatim to the server when the network is up, and the server's ack is
 * broadcast back so the emitter learns it was delivered. The relay never decrypts a text
 * and never logs or shows one.
 *
 * All state is confined to [scope] and no work switches dispatchers; production passes a
 * single-threaded scope, so the snapshot needs no locking. [onNetworkAvailable] and
 * [markOwn] may be called from any thread: the first only sends a conflated wake-up, the
 * second guards the own-text set with [ownLock].
 *
 * [log] receives one line per event: the first 8 hex chars of the text hash, the hops and
 * the outcome. Never a text, never an exception message.
 */
class MeshRelay(
    private val mesh: RelayMesh,
    private val uploader: RelayUploader,
    private val network: NetworkStatus,
    private val store: RelayStore,
    private val scope: CoroutineScope,
    private val nowMs: () -> Long = System::currentTimeMillis,
    private val log: (String) -> Unit = {},
) {
    private val mutableRelayed = MutableStateFlow(0)

    /** Distinct SEAL texts this phone delivered to the server. */
    val relayedCount: StateFlow<Int> = mutableRelayed.asStateFlow()

    private val mutablePending = MutableStateFlow(0)

    /** Heard texts not yet delivered or rejected, including the one being uploaded. */
    val pendingCount: StateFlow<Int> = mutablePending.asStateFlow()

    /** Read and written only from coroutines on [scope]. */
    private var snapshot = RelaySnapshot()

    private val started = AtomicBoolean(false)

    /** Wakes the worker: a new pending item or [onNetworkAvailable]. */
    private val wake = Channel<Unit>(Channel.CONFLATED)

    /** Guards [own]; no caller code runs under it. */
    private val ownLock = Any()

    /** Hashes of the texts this phone broadcast itself, oldest first, at most [RelayLimits.MAX_OWN]. In memory only. */
    private val own = LinkedHashSet<String>()

    /**
     * Restores the stored snapshot, then subscribes to the mesh before returning, so
     * `relay.start(); gateway.start()` misses no early broadcast and every heard text meets the
     * restored seen-set. Then starts the upload worker on [scope]. Later calls do nothing.
     */
    fun start() {
        if (!started.compareAndSet(false, true)) return
        // UNDISPATCHED runs up to the first suspension, the restore and the SharedFlow subscription, on
        // this thread. The worker is launched after it, so it starts from the restored snapshot.
        scope.launch(start = CoroutineStart.UNDISPATCHED) {
            restore()
            listen()
        }
        scope.launch { work() }
    }

    /** The network became usable: the worker re-checks right away. */
    fun onNetworkAvailable() {
        wake.trySend(Unit)
    }

    /** [text] is this phone's own emergency: it is never relayed. */
    fun markOwn(text: String) {
        val hash = relayHash(text)
        synchronized(ownLock) {
            own.remove(hash)
            own.add(hash)
            while (own.size > RelayLimits.MAX_OWN) own.remove(own.first())
        }
    }

    private fun isOwn(hash: String): Boolean = synchronized(ownLock) { hash in own }

    /**
     * Loads the stored snapshot, ordered by timestamp and capped, and drops what expired while
     * the relay was down. A missing, unparseable or other-version store starts empty and is
     * left as it is until the next change overwrites it.
     *
     * Runs on the thread that calls [start], inside the UNDISPATCHED launch, before any other
     * coroutine on [scope] touches the snapshot. The worker and every later collector step run
     * after a dispatch onto [scope], and that hand-off gives them a happens-before on the
     * snapshot written here, so no lock is needed.
     */
    private fun restore() {
        val loaded = guard(null) { store.load()?.let { RelayJson.decodeFromString(RelaySnapshot.serializer(), it) } }
            ?.takeIf { it.v == RelaySnapshot().v }
            ?: RelaySnapshot()
        val restored = pruned(loaded.copy(pending = loadedPending(loaded.pending), seen = loadedSeen(loaded.seen)), nowMs())
        if (restored == loaded) show(restored) else commit(restored)
        wake.trySend(Unit)
    }

    /** Drops the entries that expired by [now]; saves only when one did. */
    private fun prune(now: Long) {
        val next = pruned(snapshot, now)
        if (next != snapshot) commit(next)
    }

    /** [from] without its expired pending items and seen entries. */
    private fun pruned(from: RelaySnapshot, now: Long): RelaySnapshot {
        val (expired, pending) = from.pending.partition { isExpired(it.heardAtMs, now) }
        for (item in expired) note(relayHash(item.text), item.hops, "expired")
        return from.copy(pending = pending, seen = from.seen.filterNot { isExpired(it.firstSeenAtMs, now) })
    }

    /** A failing flow ends only the listener; the worker keeps delivering what is queued. */
    private suspend fun listen() {
        mesh.broadcasts.catch { if (it is CancellationException) throw it }.collect(::onHeard)
    }

    private fun onHeard(broadcast: MeshBroadcast) {
        if (broadcast.senderPeerId == guard(null) { mesh.ownPeerId }) return
        val text = broadcast.text
        if (!isRelayable(text)) return
        val hash = relayHash(text)
        if (isOwn(hash)) return
        val now = nowMs()
        // An expired seen entry no longer blocks the text, even while the worker sleeps.
        prune(now)
        val known = snapshot.seen.firstOrNull { it.hash == hash }
        if (known != null) return repeatAck(known, broadcast.relayHops, now)
        // Still queued although its seen entry was evicted: one slot per text.
        if (snapshot.pending.any { it.text == text }) return
        val hops = broadcast.relayHops
        commit(
            snapshot.copy(
                pending = appended(snapshot.pending, PendingUpload(text, hops, now, nextAttemptAtMs = now), RelayLimits.MAX_PENDING),
                seen = appended(snapshot.seen, SeenText(hash, now), RelayLimits.MAX_SEEN),
            ),
        )
        note(hash, hops, "queued")
        wake.trySend(Unit)
    }

    /**
     * The emitter repeating a delivered text has not heard the ack yet: the stored ack goes
     * out again, at most once per [RelayTiming.ACK_REBROADCAST_MIN_INTERVAL_MS]. Only a
     * broadcast the mesh accepted starts the interval.
     */
    private fun repeatAck(entry: SeenText, hops: Int, now: Long) {
        val ack = entry.ackText ?: return
        // A clock that went backwards never blocks the ack.
        if (now - entry.lastAckBroadcastAtMs in 0 until RelayTiming.ACK_REBROADCAST_MIN_INTERVAL_MS) return
        if (!broadcast(ack)) return
        commit(snapshot.copy(seen = snapshot.seen.map { if (it.hash == entry.hash) it.copy(lastAckBroadcastAtMs = now) else it }))
        note(entry.hash, hops, "ack repeated")
    }

    /**
     * Drops expired entries, uploads the oldest due item while online, one at a time, then
     * waits for whichever comes first: the next retry, the next expiry, the offline poll, or a
     * wake-up.
     */
    private suspend fun work() {
        while (true) {
            prune(nowMs())
            val online = guard(false) { network.isOnline() }
            val now = nowMs()
            val due = if (online) snapshot.pending.firstOrNull { it.nextAttemptAtMs <= now } else null
            if (due != null) {
                attempt(due)
                continue
            }
            val waitMs = waitMs(online, now)
            if (waitMs == null) wake.receive() else withTimeoutOrNull(waitMs) { wake.receive() }
        }
    }

    /** Null (wait for a wake-up only) when nothing is pending; otherwise a positive gap. */
    private fun waitMs(online: Boolean, now: Long): Long? {
        if (snapshot.pending.isEmpty()) return null
        var wait: Long? = if (online) null else RelayTiming.OFFLINE_POLL_MS
        for (item in snapshot.pending) {
            // Offline, an item already due waits for the poll, never for a gap <= 0.
            val gap = item.nextAttemptAtMs - now
            if (gap > 0) wait = minOf(wait ?: gap, gap)
            // An item in backoff still leaves the queue the moment it expires.
            val expiresIn = item.heardAtMs + RelayTiming.ENTRY_TTL_MS - now
            if (expiresIn > 0) wait = minOf(wait ?: expiresIn, expiresIn)
        }
        return wait
    }

    private suspend fun attempt(item: PendingUpload) {
        val result = try {
            uploader.upload(item.text, item.hops)
        } catch (e: CancellationException) {
            // The relay itself is being cancelled: rethrow. A stray one from the uploader is a failed try,
            // so it never stops the worker for good.
            currentCoroutineContext().ensureActive()
            note(relayHash(item.text), item.hops, "upload cancelled")
            UploadResult.Failed
        } catch (e: Exception) {
            UploadResult.Failed
        }
        val now = nowMs()
        when (result) {
            is UploadResult.Acked -> if (isAck(result.ackText)) delivered(item, result.ackText, now) else failed(item, now)
            UploadResult.Rejected -> rejected(item)
            UploadResult.Failed -> failed(item, now)
        }
    }

    /**
     * Applies even when the item left the queue during the upload: the server has it. A seen
     * entry evicted while the item waited is re-added as the newest, stamped [now], so neither
     * this cap nor a later one (after a restart sorts by time) drops it before the next text:
     * the delivery must keep a seen record, or the emitter's repeat would upload it again.
     */
    private fun delivered(item: PendingUpload, ack: String, now: Long) {
        val hash = relayHash(item.text)
        val sent = broadcast(ack)
        val known = snapshot.seen.any { it.hash == hash }
        val seen = if (known) {
            snapshot.seen.map { if (it.hash == hash) it.copy(ackText = ack, lastAckBroadcastAtMs = if (sent) now else it.lastAckBroadcastAtMs) else it }
        } else {
            appended(snapshot.seen, SeenText(hash, now, ack, if (sent) now else 0L), RelayLimits.MAX_SEEN)
        }
        commit(
            snapshot.copy(
                pending = snapshot.pending.filterNot { it.text == item.text },
                seen = seen,
                relayedCount = snapshot.relayedCount + 1,
            ),
        )
        note(hash, item.hops, if (sent) "acked" else "acked, ack not broadcast")
    }

    /** Permanent: the item is dropped and its seen entry stays, so it is never uploaded again. */
    private fun rejected(item: PendingUpload) {
        commit(snapshot.copy(pending = snapshot.pending.filterNot { it.text == item.text }))
        note(relayHash(item.text), item.hops, "rejected")
    }

    private fun failed(item: PendingUpload, now: Long) {
        val current = snapshot.pending.firstOrNull { it.text == item.text } ?: return
        val attempts = current.attempts + 1
        val retry = current.copy(attempts = attempts, nextAttemptAtMs = now + RelayTiming.retryDelayMs(attempts))
        commit(snapshot.copy(pending = snapshot.pending.map { if (it.text == item.text) retry else it }))
        note(relayHash(item.text), item.hops, "failed $attempts")
    }

    private fun commit(next: RelaySnapshot) {
        show(next)
        guard(Unit) { store.save(RelayJson.encodeToString(RelaySnapshot.serializer(), next)) }
    }

    /** Makes [next] current without saving it: only for a snapshot equal to the stored one. */
    private fun show(next: RelaySnapshot) {
        snapshot = next
        mutablePending.value = next.pending.size
        mutableRelayed.value = next.relayedCount
    }

    private fun broadcast(text: String): Boolean = guard(false) { mesh.broadcast(text) }

    private fun note(hash: String, hops: Int, outcome: String) {
        guard(Unit) { log("${hash.take(8)} hops=$hops $outcome") }
    }

    private companion object {
        /** A clock that went backwards never expires an entry early. */
        fun isExpired(atMs: Long, now: Long): Boolean = now - atMs >= RelayTiming.ENTRY_TTL_MS

        /** A loaded queue, oldest heard first, at most [RelayLimits.MAX_PENDING]: the oldest heard are dropped. */
        fun loadedPending(items: List<PendingUpload>): List<PendingUpload> =
            items.sortedBy { it.heardAtMs }.takeLast(RelayLimits.MAX_PENDING)

        /** A loaded seen-set, oldest first seen first, at most [RelayLimits.MAX_SEEN]: the oldest first seen are evicted. */
        fun loadedSeen(entries: List<SeenText>): List<SeenText> =
            entries.sortedBy { it.firstSeenAtMs }.takeLast(RelayLimits.MAX_SEEN)

        /**
         * [list] with [entry] added last, keeping the newest [max] by arrival: the earliest added
         * go first. A new entry is never dropped on arrival, even stamped earlier than the rest
         * after the clock stepped back.
         */
        fun <T> appended(list: List<T>, entry: T, max: Int): List<T> = (list + entry).takeLast(max)

        /** At most [RelayLimits.MAX_TEXT_CHARS] chars and a SEAL frame with a 146-byte box. */
        fun isRelayable(text: String): Boolean =
            // Caps parsing cost on junk. parseMesh trims whitespace, so a padded valid SEAL is the only text it alone stops (test: ignoresOversizedPaddedSeal).
            text.length <= RelayLimits.MAX_TEXT_CHARS &&
                guard(false) { (MamaText.parseMesh(text) as? MeshFrame.Sealed)?.box?.size == RelayLimits.SEALED_BOX_BYTES }

        /** A `MAMA1:ACK:` text with a 76-byte ack; anything else is not a delivery. */
        fun isAck(text: String): Boolean =
            text.length <= RelayLimits.MAX_TEXT_CHARS &&
                guard(false) { (MamaText.parseMesh(text) as? MeshFrame.Ack)?.ack?.size == RelayLimits.ACK_BYTES }

        /** Port calls never crash the relay: any failure becomes [fallback]; cancellation is rethrown. */
        inline fun <T> guard(fallback: T, block: () -> T): T =
            try {
                block()
            } catch (e: CancellationException) {
                throw e
            } catch (e: Exception) {
                fallback
            }
    }
}
