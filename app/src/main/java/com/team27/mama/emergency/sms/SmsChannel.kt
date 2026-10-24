package com.team27.mama.emergency.sms

import com.team27.mama.emergency.payload.AckInfo
import com.team27.mama.emergency.payload.MamaText

/** How one emission ended. */
sealed interface SmsOutcome {
    /** The server acked this emergency at [ack].seq, at or above the seq being sent. */
    data class Acked(val ack: AckInfo) : SmsOutcome

    /** Every attempt went out (or failed) and no ack came within the last wait. [sent] counts radio-accepted sends. */
    data class Exhausted(val attempts: Int, val sent: Int) : SmsOutcome

    data object Cancelled : SmsOutcome

    /** A newer [SmsChannel.emit] replaced this one. */
    data object Superseded : SmsOutcome

    /** No central number configured: nothing was sent. */
    data object NoNumber : SmsOutcome
}

/** One in-flight emission. [outcome] is null while it is still sending or waiting. */
interface SmsEmission {
    val outcome: SmsOutcome?
    fun cancel()
}

/**
 * The SMS channel of the offline emergency (#58), for when there is no data but
 * there is cellular signal. #59's orchestrator calls [emit] in parallel with the
 * mesh. The text is `MAMA1 ` + Base64url(signed payload): signed, not
 * encrypted, one 137-char SMS.
 *
 * Schedule: send, wait [retryDelaysMs] (default 10 s, 30 s, 90 s) between sends,
 * so at most 1 + [retryDelaysMs].size sends, then [ackWindowMs] for a last ack
 * before [SmsOutcome.Exhausted]. A failed send uses up its attempt and backs off
 * like a sent one. A server ack for the same emergency with seq at or above the
 * one being sent stops it at once.
 *
 * One emission at a time: a new [emit] (for example the same emergency at seq+1)
 * supersedes the one in flight. The central number is read at emit time from
 * remote config (#55), never hard-coded.
 *
 * Sends SEND_SMS in the background with no user interaction, which is why MAMÁ
 * is sideloaded only (Play does not allow SEND_SMS for this use).
 */
class SmsChannel(
    private val sender: SmsSender,
    private val centralNumber: () -> String,
    private val scheduler: SmsScheduler,
    acks: SmsAckBus,
    private val retryDelaysMs: List<Long> = DEFAULT_RETRY_DELAYS_MS,
    private val ackWindowMs: Long = DEFAULT_ACK_WINDOW_MS,
) {
    private var current: Emission? = null

    init {
        acks.subscribe { ack -> synchronized(this) { current }?.onAck(ack) }
    }

    /** Starts sending [signed] (a signed payload) for [emergencyId] at [seq]; [onOutcome] runs once when it ends. */
    fun emit(
        signed: ByteArray,
        emergencyId: Long,
        seq: Int,
        onOutcome: (SmsOutcome) -> Unit = {},
    ): SmsEmission {
        val text = MamaText.sms(signed)
        require(text.length <= MAX_SMS_CHARS) { "SMS text is ${text.length} chars, the limit is one SMS" }
        val number = centralNumber().trim()
        val emission = Emission(text, emergencyId, seq, number, onOutcome)
        val previous = synchronized(this) { current.also { current = emission } }
        previous?.finish(SmsOutcome.Superseded)
        if (number.isEmpty()) emission.finish(SmsOutcome.NoNumber) else emission.attempt()
        return emission
    }

    /** Cancels the emission in flight, if any. */
    fun cancel() {
        synchronized(this) { current }?.cancel()
    }

    private fun cleared(emission: Emission) = synchronized(this) {
        if (current === emission) current = null
    }

    private inner class Emission(
        private val text: String,
        private val emergencyId: Long,
        private val seq: Int,
        private val number: String,
        private val onOutcome: (SmsOutcome) -> Unit,
    ) : SmsEmission {
        private val lock = Any()
        private var attempts = 0
        private var sent = 0
        private var pending: Cancellable? = null

        @Volatile
        override var outcome: SmsOutcome? = null
            private set

        fun attempt() {
            val n = synchronized(lock) {
                if (outcome != null) return
                ++attempts
            }
            sender.send(number, text) { ok -> afterSend(n, ok) }
        }

        private fun afterSend(n: Int, ok: Boolean) {
            synchronized(lock) {
                if (outcome != null) return
                if (ok) sent++
                val last = n > retryDelaysMs.size
                val delay = if (last) ackWindowMs else retryDelaysMs[n - 1]
                pending = scheduler.schedule(delay) {
                    if (last) finish(SmsOutcome.Exhausted(n, synchronized(lock) { sent })) else attempt()
                }
            }
        }

        fun onAck(ack: AckInfo) {
            if (ack.emergencyId == emergencyId && ack.seq >= seq) finish(SmsOutcome.Acked(ack))
        }

        override fun cancel() = finish(SmsOutcome.Cancelled)

        fun finish(result: SmsOutcome) {
            val timer = synchronized(lock) {
                if (outcome != null) return
                outcome = result
                pending.also { pending = null }
            }
            timer?.cancel()
            cleared(this)
            onOutcome(result)
        }
    }

    companion object {
        val DEFAULT_RETRY_DELAYS_MS = listOf(10_000L, 30_000L, 90_000L)
        const val DEFAULT_ACK_WINDOW_MS = 90_000L
        const val MAX_SMS_CHARS = 160
    }
}
