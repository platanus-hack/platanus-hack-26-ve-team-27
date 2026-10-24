package com.team27.mama.relay

import com.team27.mama.emergency.payload.MamaText
import com.team27.mama.emergency.payload.MeshFrame
import com.team27.mama.mesh.MeshBroadcast
import com.team27.mama.onboarding.NetworkStatus
import java.util.concurrent.atomic.AtomicBoolean
import kotlin.coroutines.cancellation.CancellationException
import kotlinx.coroutines.CoroutineScope
import kotlinx.coroutines.channels.Channel
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

    /** Subscribes to the mesh and starts the upload worker on [scope]; later calls do nothing. */
    fun start() {
        if (!started.compareAndSet(false, true)) return
        scope.launch { listen() }
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
        val known = snapshot.seen.firstOrNull { it.hash == hash }
        if (known != null) return repeatAck(known, broadcast.relayHops, now)
        val hops = broadcast.relayHops
        commit(
            snapshot.copy(
                pending = (snapshot.pending + PendingUpload(text, hops, now, nextAttemptAtMs = now)).takeLast(RelayLimits.MAX_PENDING),
                seen = (snapshot.seen + SeenText(hash, now)).takeLast(RelayLimits.MAX_SEEN),
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
     * Uploads the oldest due item while online, one at a time, then waits for whichever
     * comes first: the next retry, the offline poll, or a wake-up.
     */
    private suspend fun work() {
        while (true) {
            // Task 3: prune expired entries here.
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
        }
        return wait
    }

    private suspend fun attempt(item: PendingUpload) {
        val result = try {
            uploader.upload(item.text, item.hops)
        } catch (e: CancellationException) {
            // Rethrow cancellation explicitly; never treat it as a failed upload.
            throw e
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

    /** Applies even when the item left the queue during the upload: the server has it. */
    private fun delivered(item: PendingUpload, ack: String, now: Long) {
        val hash = relayHash(item.text)
        val sent = broadcast(ack)
        val known = snapshot.seen.any { it.hash == hash }
        val seen = if (known) {
            snapshot.seen.map { if (it.hash == hash) it.copy(ackText = ack, lastAckBroadcastAtMs = if (sent) now else it.lastAckBroadcastAtMs) else it }
        } else {
            (snapshot.seen + SeenText(hash, item.heardAtMs, ack, if (sent) now else 0L)).takeLast(RelayLimits.MAX_SEEN)
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
        snapshot = next
        mutablePending.value = next.pending.size
        mutableRelayed.value = next.relayedCount
        guard(Unit) { store.save(RelayJson.encodeToString(RelaySnapshot.serializer(), next)) }
    }

    private fun broadcast(text: String): Boolean = guard(false) { mesh.broadcast(text) }

    private fun note(hash: String, hops: Int, outcome: String) {
        guard(Unit) { log("${hash.take(8)} hops=$hops $outcome") }
    }

    private companion object {
        /** At most [RelayLimits.MAX_TEXT_CHARS] chars and a SEAL frame with a 146-byte box. */
        fun isRelayable(text: String): Boolean =
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
