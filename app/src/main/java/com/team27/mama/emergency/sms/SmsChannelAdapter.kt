package com.team27.mama.emergency.sms

import com.team27.mama.emergency.SmsEmergencyChannel
import com.team27.mama.emergency.payload.MamaText
import com.team27.mama.emergency.payload.PayloadCodec
import com.team27.mama.emergency.payload.PayloadFormat
import kotlinx.coroutines.CompletableDeferred
import kotlinx.coroutines.channels.awaitClose
import kotlinx.coroutines.flow.Flow
import kotlinx.coroutines.flow.callbackFlow
import kotlinx.coroutines.flow.emptyFlow

/**
 * The orchestrator's SMS port (#59) over #58's [SmsChannel]. Both are null with the
 * OFFLINE_EMERGENCY_ENABLED flag off, and then SMS is never available.
 *
 * One #58 emission per frame: [send] starts it and suspends until it ends, so #58 keeps its
 * own retry schedule and ack wait. The same text again while that emission is in flight joins
 * it instead of restarting the schedule; a different text (the UPDATE or CANCEL frame)
 * supersedes it, as #58 intends. When every waiter of an emission is cancelled (the
 * orchestrator stopped that frame), the emission is cancelled too.
 *
 * [send]'s number is ignored: #58 reads the central number from remote config at emit time,
 * the same [centralNumber] that [canSend] checks.
 *
 * [incoming] carries the bodies [SmsAckReceiver] already verified ([SmsAckFilter]: central
 * number and server signature); the orchestrator verifies them again with `AckReader.fromSms`.
 * Never logs, and puts in no exception text, a number, an SMS text or a location.
 */
class SmsChannelAdapter(
    private val channel: SmsChannel?,
    private val acks: SmsAckBus?,
    private val centralNumber: () -> String,
    /** SEND_SMS is granted. */
    private val sendPermitted: () -> Boolean,
) : SmsEmergencyChannel {
    private class Active(val text: String, val emission: SmsEmission, val outcome: CompletableDeferred<SmsOutcome>) {
        var waiters = 0
    }

    private val lock = Any()
    private var active: Active? = null

    override fun canSend(): Boolean = channel != null && centralNumber().isNotBlank() && sendPermitted()

    override suspend fun send(number: String, text: String): Boolean {
        val sms = channel ?: return false
        val joined = synchronized(lock) {
            val reuse = active?.takeIf { it.text == text && it.emission.outcome == null }
            (reuse ?: start(sms, text).also { active = it }).also { it.waiters++ }
        }
        try {
            return when (val outcome = joined.outcome.await()) {
                is SmsOutcome.Acked -> true
                is SmsOutcome.Exhausted -> outcome.sent > 0
                SmsOutcome.Cancelled, SmsOutcome.Superseded, SmsOutcome.NoNumber -> false
            }
        } finally {
            synchronized(lock) {
                // Under the lock, so a send() of the same text cannot join an emission being cancelled.
                // A no-op once the emission ended; otherwise the last waiter was cancelled.
                // Safe: #58's finish never takes this lock.
                if (--joined.waiters == 0) joined.emission.cancel()
            }
        }
    }

    /** Called under [lock]; [SmsChannel.emit] may finish the previous emission (Superseded) or this one at once. */
    private fun start(sms: SmsChannel, text: String): Active {
        val signed = MamaText.parseSms(text)
        val payload = PayloadCodec.decodeBody(signed.copyOfRange(0, minOf(PayloadFormat.BODY_SIZE, signed.size)))
        val outcome = CompletableDeferred<SmsOutcome>()
        val emission = sms.emit(signed, payload.emergencyId, payload.seq) { outcome.complete(it) }
        return Active(text, emission, outcome)
    }

    override val incoming: Flow<String> = acks?.let { bus ->
        callbackFlow {
            val subscription = bus.subscribeTexts { trySend(it) }
            awaitClose { subscription.cancel() }
        }
    } ?: emptyFlow()
}
