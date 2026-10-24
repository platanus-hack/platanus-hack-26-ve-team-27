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
 * a valid server ack arrives by any channel.
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

    override fun declare(): Boolean {
        val id = newEmergencyId()
        val before = mutableState.getAndUpdate {
            if (it.phase.isActive) it else EmergencyState(phase = EmergencyPhase.SENDING, emergencyId = id)
        }
        if (before.phase.isActive) return false
        val next = Session(id, channelOverride(), config(), orElse(null) { location.lastKnown() })
        val frame = next.buildAlert()
        scope.launch { next.begin(frame) }
        return true
    }

    /** Stops emitting. Task 4 replaces this with a cancel frame (CANCELLING, then CANCELLED). */
    override fun cancel() {
        scope.launch {
            val phase = mutableState.value.phase
            if (phase == EmergencyPhase.IDLE || phase == EmergencyPhase.CANCELLING || phase == EmergencyPhase.CANCELLED) {
                return@launch
            }
            session?.jobs?.cancel()
            session = null
            mutableState.update { it.copy(phase = EmergencyPhase.CANCELLED) }
        }
    }

    private fun online(): Boolean = orElse(false) { network.isOnline() }

    private inner class Session(
        val id: Long,
        val override: ChannelOverride,
        val config: EmergencyConfig,
        val fix: LocationFix?,
    ) {
        /** Parent of the ack collectors and every emission job of this emergency. */
        val jobs = CoroutineScope(scope.coroutineContext + SupervisorJob(scope.coroutineContext[Job]))
        var frame: Frame? = null
        var emission: Job? = null

        /** True once `mesh.start()` succeeded; reset when a broadcast is refused. */
        var meshRunning = false

        /** Applies [f] to the state unless this session was replaced or cancelled. */
        fun update(f: (EmergencyState) -> EmergencyState) {
            if (session === this) mutableState.update(f)
        }

        fun use(channel: Channel) = update { it.copy(channelsUsed = it.channelsUsed + channel) }

        /** Frame seq 0, or null when the device key cannot sign (mesh keystore unusable). */
        fun buildAlert(): Frame? = orElse(null) {
            EmergencyFrames.build(Kind.ALERT, signer, id, 0, fix, nowMs())
        }

        fun begin(alert: Frame?) {
            session?.jobs?.cancel()
            session = this
            val serverKey = config.serverEd25519PublicKey
            jobs.launch { collectAcks(mesh.incoming) { AckReader.fromMesh(it, serverKey) } }
            jobs.launch { collectAcks(sms.incoming) { AckReader.fromSms(it, serverKey) } }
            if (alert != null) {
                emit(alert)
                return
            }
            update { it.copy(phase = EmergencyPhase.NO_CHANNEL, meshIssue = MeshIssue.UNAVAILABLE) }
            jobs.launch {
                for (gap in EmergencyTiming.reemitGaps()) {
                    delay(gap)
                    val built = buildAlert() ?: continue
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

        /** Applies an ack; true when it covers the frame being emitted, which then stops. */
        fun onAck(ack: AckInfo): Boolean {
            if (ack.emergencyId != id || session !== this) return false
            update {
                it.copy(phase = EmergencyPhase.NOTIFIED, ackedSeq = maxOf(it.ackedSeq ?: ack.seq, ack.seq))
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
                    // Rethrow cancellation, no runCatching: a swallowed cancel (ack elsewhere) must never reach the fallback.
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

        /** Port calls must never crash the emergency: any failure becomes [fallback]. */
        inline fun <T> orElse(fallback: T, block: () -> T): T =
            try {
                block()
            } catch (_: Exception) {
                fallback
            }
    }
}
