package com.team27.mama.emergency

import com.team27.mama.emergency.config.EmergencyConfig
import com.team27.mama.emergency.payload.AckInfo
import com.team27.mama.emergency.payload.Kind
import com.team27.mama.emergency.payload.PayloadCodec
import com.team27.mama.emergency.payload.PayloadSigner
import com.team27.mama.onboarding.NetworkStatus
import com.team27.mama.server.ServerHttpException
import kotlin.coroutines.cancellation.CancellationException
import kotlinx.coroutines.CoroutineScope
import kotlinx.coroutines.Job
import kotlinx.coroutines.SupervisorJob
import kotlinx.coroutines.cancel
import kotlinx.coroutines.coroutineScope
import kotlinx.coroutines.delay
import kotlinx.coroutines.flow.Flow
import kotlinx.coroutines.flow.MutableStateFlow
import kotlinx.coroutines.flow.StateFlow
import kotlinx.coroutines.flow.asStateFlow
import kotlinx.coroutines.flow.catch
import kotlinx.coroutines.flow.getAndUpdate
import kotlinx.coroutines.flow.update
import kotlinx.coroutines.launch
import kotlinx.coroutines.withTimeoutOrNull

object EmergencyTiming {
    /** One HTTP attempt; timeout, IOException or an invalid reply is a failure. */
    const val HTTP_TIMEOUT_MS = 5_000L

    /** Fallback ticks double from the initial gap up to the cap: t = 0, 5, 15, 35, 75, 135, 195 … s. */
    const val REEMIT_INITIAL_MS = 5_000L
    const val REEMIT_MAX_MS = 60_000L

    /** A last-known fix this recent counts as fresh (no update is awaited). */
    const val FRESH_FIX_MAX_AGE_MS = 30_000L

    /** How long to wait for one fresh fix after declaring. */
    const val FRESH_FIX_TIMEOUT_MS = 120_000L

    /** A cancel frame is emitted until its ack or this long, then the state is CANCELLED anyway. */
    const val CANCEL_WINDOW_MS = 120_000L

    /** The gaps between fallback ticks: 5, 10, 20, 40, 60, 60 … s. Infinite. */
    fun reemitGaps(): Sequence<Long> =
        generateSequence(REEMIT_INITIAL_MS) { (it * 2).coerceAtMost(REEMIT_MAX_MS) }
}

/**
 * Emits an emergency over every channel the override allows (#62 §2): HTTP first when
 * online, then SMS and the mesh in parallel, re-emitting on [EmergencyTiming] ticks until
 * a valid server ack arrives by any channel. A fresh fix after declaring becomes one UPDATE
 * frame (seq + 1); [cancel] replaces the frame with a signed CANCEL over the same channels.
 *
 * All work runs on [scope] and never switches dispatchers; production passes a
 * single-threaded scope, so the session state below needs no locking. [declare] and
 * [cancel] may be called from any thread.
 */
class EmergencyOrchestrator(
    private val signer: PayloadSigner,
    private val config: () -> EmergencyConfig,
    private val http: HttpEmergencyChannel,
    private val sms: SmsEmergencyChannel,
    private val mesh: MeshEmergencyChannel,
    private val location: LocationSource,
    private val network: NetworkStatus,
    private val channelOverride: () -> ChannelOverride,
    private val scope: CoroutineScope,
    private val nowMs: () -> Long = System::currentTimeMillis,
    private val newEmergencyId: () -> Long = { PayloadCodec.newEmergencyId() },
) : EmergencyControls {

    private val mutableState = MutableStateFlow(EmergencyState())
    override val state: StateFlow<EmergencyState> = mutableState.asStateFlow()

    /** The emergency in progress. Read and written only from coroutines on [scope]. */
    private var session: Session? = null

    /** Makes the phase check and change of [declare] and [cancel] one step across threads. */
    private val lock = Any()

    /** The session of the last [declare]. Guarded by [lock]. */
    private var declared: Session? = null

    override fun declare(): Boolean {
        val next = synchronized(lock) {
            val id = newEmergencyId()
            val before = mutableState.getAndUpdate {
                if (it.phase.isActive) it else EmergencyState(phase = EmergencyPhase.SENDING, emergencyId = id)
            }
            if (before.phase.isActive) return false
            Session(id, channelOverride(), config(), orElse(null) { location.lastKnown() }).also { declared = it }
        }
        scope.launch { next.begin() }
        return true
    }

    /**
     * CANCELLING right away, on the calling thread, so a second cancel or a declare sees it.
     * The scope then cancels exactly the session that was declared, never a later one.
     */
    override fun cancel() {
        val target = synchronized(lock) {
            val before = mutableState.getAndUpdate {
                if (it.phase.isCancellable) it.copy(phase = EmergencyPhase.CANCELLING) else it
            }
            if (!before.phase.isCancellable) return
            declared
        } ?: return
        scope.launch { target.cancel() }
    }

    private fun online(): Boolean = orElse(false) { network.isOnline() }

    private inner class Session(
        val id: Long,
        val override: ChannelOverride,
        val config: EmergencyConfig,
        /** The newest fix: frames built from now on carry it. */
        var fix: LocationFix?,
    ) {
        /** Parent of the ack collectors and every emission job of this emergency. */
        val jobs = CoroutineScope(scope.coroutineContext + SupervisorJob(scope.coroutineContext[Job]))
        var frame: Frame? = null
        var emission: Job? = null

        /** Retries building a frame while the device key cannot sign. */
        var pending: Job? = null
        var fixWait: Job? = null

        /** seq of the cancel frame once [cancel] ran; acks then end the emergency instead of notifying. */
        var cancelSeq: Int? = null

        /** True once `mesh.start()` succeeded; reset when a broadcast is refused. */
        var meshRunning = false

        /** Applies [f] to the state unless this session was replaced or cancelled. */
        fun update(f: (EmergencyState) -> EmergencyState) {
            if (session === this) mutableState.update(f)
        }

        fun use(channel: Channel) = update { it.copy(channelsUsed = it.channelsUsed + channel) }

        /** A signed frame, or null when the device key cannot sign (mesh keystore unusable). */
        fun build(kind: Kind, seq: Int): Frame? = orElse(null) {
            EmergencyFrames.build(kind, signer, id, seq, fix, nowMs())
        }

        fun begin() {
            val declaredState = mutableState.value
            if (declaredState.emergencyId != id || !declaredState.phase.isActive) return
            session?.jobs?.cancel()
            session = this
            // Cancelled before this ran: nothing went out, so there is nothing to cancel.
            if (declaredState.phase == EmergencyPhase.CANCELLING) return finish()
            val serverKey = config.serverEd25519PublicKey
            jobs.launch { collectAcks(mesh.incoming) { AckReader.fromMesh(it, serverKey) } }
            jobs.launch { collectAcks(sms.incoming) { AckReader.fromSms(it, serverKey) } }
            val known = fix
            if (known == null || nowMs() - known.timeMs > EmergencyTiming.FRESH_FIX_MAX_AGE_MS) {
                fixWait = jobs.launch { awaitFreshFix() }
            }
            emitSigned { build(Kind.ALERT, 0) }
        }

        /** One fresh fix within [EmergencyTiming.FRESH_FIX_TIMEOUT_MS] becomes the UPDATE frame. */
        suspend fun awaitFreshFix() {
            val fresh = try {
                withTimeoutOrNull(EmergencyTiming.FRESH_FIX_TIMEOUT_MS) { location.awaitFreshFix() }
            } catch (e: CancellationException) {
                throw e
            } catch (_: Exception) {
                null
            } ?: return
            if (session !== this || cancelSeq != null) return
            val used = fix
            if (used != null && fresh.timeMs <= used.timeMs) return
            fix = fresh
            // Still waiting for the key to sign the alert: the alert will carry this fix.
            val current = frame ?: return
            emitSigned { build(Kind.UPDATE, current.payload.seq + 1) }
        }

        /** Stops the fix wait and the current frame, then emits CANCEL until its ack or the window ends. */
        fun cancel() {
            if (session !== this || cancelSeq != null) return
            fixWait?.cancel()
            emission?.cancel()
            val sent = frame ?: return finish()
            val seq = sent.payload.seq + 1
            cancelSeq = seq
            fix = listOfNotNull(fix, orElse(null) { location.lastKnown() }).maxByOrNull { it.timeMs }
            emitSigned { build(Kind.CANCEL, seq) }
            jobs.launch {
                delay(EmergencyTiming.CANCEL_WINDOW_MS)
                finish()
            }
        }

        /** CANCELLED: every job of this emergency stops and [declare] works again. */
        fun finish() {
            if (session !== this) return
            mutableState.update { it.copy(phase = EmergencyPhase.CANCELLED) }
            session = null
            jobs.cancel()
        }

        /**
         * Emits the frame [make] builds. While the key cannot sign, the frame already being
         * emitted (if any) keeps going and [make] is retried on the re-emit gaps.
         */
        fun emitSigned(make: () -> Frame?) {
            pending?.cancel()
            make()?.let { return emit(it) }
            if (emission?.isActive != true) {
                update {
                    val phase = if (it.phase in FALLBACK_PHASES) EmergencyPhase.NO_CHANNEL else it.phase
                    it.copy(phase = phase, meshIssue = MeshIssue.UNAVAILABLE)
                }
            }
            pending = jobs.launch {
                for (gap in EmergencyTiming.reemitGaps()) {
                    delay(gap)
                    val built = make() ?: continue
                    update { it.copy(meshIssue = null) }
                    emit(built)
                    return@launch
                }
            }
        }

        /** Replaces the frame being emitted: the previous emission job stops first. */
        fun emit(next: Frame) {
            emission?.cancel()
            frame = next
            update { it.copy(seq = next.payload.seq) }
            emission = jobs.launch { Emission(next).run() }
        }

        suspend fun collectAcks(texts: Flow<String>, read: (String) -> AckInfo?) {
            texts.catch { }.collect { text -> read(text)?.let(::onAck) }
        }

        /**
         * Applies an ack; true when it covers the frame being emitted, which then stops.
         * While cancelling, an ack covering the cancel ends the emergency; an older one changes only [EmergencyState.ackedSeq].
         */
        fun onAck(ack: AckInfo): Boolean {
            if (ack.emergencyId != id || session !== this) return false
            val cancelling = cancelSeq
            update {
                val acked = maxOf(it.ackedSeq ?: ack.seq, ack.seq)
                if (cancelling != null) it.copy(ackedSeq = acked) else it.copy(phase = EmergencyPhase.NOTIFIED, ackedSeq = acked)
            }
            if (cancelling != null) {
                if (ack.seq < cancelling) return false
                finish()
                return true
            }
            val current = frame ?: return false
            if (ack.seq < current.payload.seq) return false
            emission?.cancel()
            return true
        }

        /** One frame: an HTTP attempt, then the fallback tick loop until an ack or a new frame. */
        inner class Emission(private val frame: Frame) {
            private var meshText: String? = null
            private var meshQueued = false
            private var smsStep = SmsStep.NOT_TRIED
            private var httpRetry: Job? = null

            /** The server rejected this frame (4xx): retrying cannot help. */
            private var httpRejected = false

            suspend fun run() = coroutineScope {
                val httpFirst = override.allowsHttp && online()
                if (httpFirst) {
                    update { if (it.phase in FALLBACK_PHASES) it.copy(phase = EmergencyPhase.SENDING) else it }
                    if (attemptHttp()) return@coroutineScope
                }
                // The first tick right after a failed attempt would only repeat it.
                var retryHttp = !httpFirst
                for (gap in EmergencyTiming.reemitGaps()) {
                    tick(this, retryHttp)
                    retryHttp = true
                    delay(gap)
                }
            }

            /** True when the reply was an ack that ends this frame; anything else is a failure. */
            private suspend fun attemptHttp(): Boolean {
                use(Channel.HTTP)
                val reply = withTimeoutOrNull(EmergencyTiming.HTTP_TIMEOUT_MS) {
                    // Rethrow cancellation explicitly; never treat it as a channel failure.
                    try {
                        http.post(frame.smsText)
                    } catch (e: CancellationException) {
                        throw e
                    } catch (e: ServerHttpException) {
                        if (e.code in 400..499) httpRejected = true
                        null
                    } catch (_: Exception) {
                        null
                    }
                } ?: return false
                val ack = AckReader.fromHttp(reply, config.serverEd25519PublicKey) ?: return false
                return onAck(ack)
            }

            private fun tick(children: CoroutineScope, retryHttp: Boolean) {
                if (smsStep == SmsStep.NOT_TRIED) sendSms(children)
                broadcastMesh()
                if (retryHttp && override.allowsHttp && !httpRejected && httpRetry?.isActive != true && online()) {
                    httpRetry = children.launch { attemptHttp() }
                }
                refreshPhase()
            }

            /** Runs in its own child so a slow send never delays the mesh broadcast. */
            private fun sendSms(children: CoroutineScope) {
                val number = config.smsNumber
                if (!override.allowsSms || number.isBlank() || !orElse(false) { sms.canSend() }) return
                smsStep = SmsStep.IN_FLIGHT
                use(Channel.SMS)
                children.launch {
                    val sent = try {
                        sms.send(number, frame.smsText)
                    } catch (e: CancellationException) {
                        throw e
                    } catch (_: Exception) {
                        false
                    }
                    smsStep = if (sent) SmsStep.SENT else SmsStep.FAILED
                    refreshPhase()
                }
            }

            private fun broadcastMesh() {
                if (!override.allowsMesh) return
                val text = meshText ?: config.serverX25519PublicKey
                    ?.let { key -> orElse(null) { frame.meshText(key) } }
                    ?.also { meshText = it }
                if (text == null) {
                    update { it.copy(meshIssue = MeshIssue.NO_SERVER_KEYS) }
                    return
                }
                if (!meshRunning) meshRunning = orElse(false) { mesh.start() }
                meshQueued = meshRunning && orElse(false) { mesh.broadcast(text) }
                if (meshRunning) use(Channel.MESH)
                if (!meshQueued) meshRunning = false
                update { it.copy(meshIssue = if (meshQueued) null else MeshIssue.UNAVAILABLE) }
            }

            private fun refreshPhase() {
                val searching = meshQueued || smsStep == SmsStep.IN_FLIGHT || smsStep == SmsStep.SENT
                val phase = if (searching) EmergencyPhase.SEARCHING_NEARBY else EmergencyPhase.NO_CHANNEL
                update { if (it.phase in FALLBACK_PHASES) it.copy(phase = phase) else it }
            }
        }
    }

    private enum class SmsStep { NOT_TRIED, IN_FLIGHT, SENT, FAILED }

    private companion object {
        /** Phases the fallback may overwrite; NOTIFIED and cancel phases are never downgraded. */
        val FALLBACK_PHASES = setOf(EmergencyPhase.SENDING, EmergencyPhase.SEARCHING_NEARBY, EmergencyPhase.NO_CHANNEL)

        /** An emergency that a cancel can still stop. */
        val EmergencyPhase.isCancellable: Boolean get() = isActive && this != EmergencyPhase.CANCELLING

        /** Port calls must never crash the emergency: any failure becomes [fallback]. */
        inline fun <T> orElse(fallback: T, block: () -> T): T =
            try {
                block()
            } catch (_: Exception) {
                fallback
            }
    }
}
