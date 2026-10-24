package com.team27.mama.emergency.sms

import com.team27.mama.emergency.config.RemoteConfigRepository
import com.team27.mama.emergency.payload.AckInfo
import com.team27.mama.emergency.payload.MamaText
import com.team27.mama.emergency.payload.PayloadCodec
import com.team27.mama.emergency.payload.PayloadException
import com.team27.mama.emergency.payload.PayloadFormat
import kotlinx.coroutines.CompletableDeferred
import kotlinx.coroutines.Deferred
import kotlinx.coroutines.async
import kotlinx.coroutines.coroutineScope
import kotlinx.coroutines.selects.select
import kotlinx.coroutines.withTimeoutOrNull

/** Sends one text message. The Android implementation is [SmsManagerSender]. */
interface SmsSender {
    /** False when the app may not send SMS (SEND_SMS not granted): retrying cannot help. */
    fun canSend(): Boolean

    /**
     * True when the radio accepted the message (SmsManager's sent status, not a delivery report).
     * Never throws except for cancellation.
     */
    suspend fun sendText(to: String, text: String): Boolean
}

sealed interface SmsSendResult {
    /** The radio accepted the SMS on attempt number [attempts] (1 = first try). */
    data class Sent(val attempts: Int) : SmsSendResult

    /** Every attempt (the first try plus the retries in [SmsChannel.backoffMillis]) failed. */
    data class Failed(val attempts: Int) : SmsSendResult

    /** [SmsChannel.stop] was called before an attempt succeeded (an attempt in flight was cut). */
    data class Stopped(val attempts: Int) : SmsSendResult

    /** The remote config has no usable central number. Nothing was sent. */
    data object NoDestination : SmsSendResult

    /** SEND_SMS is not granted. Nothing was sent. */
    data object NoPermission : SmsSendResult

    /** The text is over [SmsChannel.MAX_SMS_CHARS], so it would not be one SMS. Nothing was sent. */
    data class TooLong(val length: Int) : SmsSendResult
}

/**
 * The SMS leg of the offline emergency (#58): the signed payload (ADR 0001) as one
 * `MAMA1 <Base64url>` text to the central number, in the background, with retries.
 *
 * How #59 calls it: when HTTP fails and there is cell signal, in parallel with the mesh,
 * `launch { channel.send(signed) }` with the 98-byte signed payload from `PayloadCodec.sign`
 * (the same bytes HTTP and the mesh carry). [send] suspends until the radio accepts the SMS,
 * every attempt fails, or it is stopped.
 *
 * How the ack listener stops it: [SmsAckReceiver] publishes the verified ack on [OfflineAcks] and
 * AppGraph routes it to [onAck], which calls [stop] when the ack is for the emergency being sent.
 * Cancelling the coroutine that runs [send] works too. Either way no further attempt starts and
 * an attempt in flight is cut; an SMS already handed to the radio cannot be recalled.
 *
 * One [send] at a time per channel: [stop] targets the send in progress.
 */
class SmsChannel(
    private val remoteConfig: RemoteConfigRepository,
    private val sender: SmsSender,
    /** An attempt whose sent status has not arrived after this long counts as failed. Injected for tests. */
    private val attemptTimeoutMillis: Long = ATTEMPT_TIMEOUT_MILLIS,
    /** Waits [millis] between attempts, returning early when [stop] completes. Injected for tests. */
    private val sleeper: suspend (millis: Long, stop: Deferred<Unit>) -> Unit = ::sleepUnlessStopped,
) {
    @Volatile
    private var stopSignal = CompletableDeferred<Unit>()

    /** Emergency id of the payload being sent, null when idle or when the payload cannot be decoded. */
    @Volatile
    private var sendingEmergencyId: Long? = null

    /**
     * Signed payload to SMS. The destination is `remoteConfig.current().smsNumber`, read once per
     * call; a blank number sends nothing. Never throws except for cancellation.
     */
    suspend fun send(signed: ByteArray): SmsSendResult {
        val text = MamaText.sms(signed)
        if (text.length > MAX_SMS_CHARS) return SmsSendResult.TooLong(text.length)
        val to = remoteConfig.current().smsNumber.trim()
        if (to.isEmpty()) return SmsSendResult.NoDestination
        if (!sender.canSend()) return SmsSendResult.NoPermission

        val stop = CompletableDeferred<Unit>().also { stopSignal = it }
        sendingEmergencyId = emergencyIdOf(signed)
        try {
            return attempts(to, text, stop)
        } finally {
            sendingEmergencyId = null
        }
    }

    private suspend fun attempts(to: String, text: String, stop: Deferred<Unit>): SmsSendResult {
        var attempts = 0
        while (true) {
            if (stop.isCompleted) return SmsSendResult.Stopped(attempts)
            attempts++
            when (attempt(to, text, stop)) {
                true -> return SmsSendResult.Sent(attempts)
                null -> return SmsSendResult.Stopped(attempts)
                false -> Unit
            }
            val retry = attempts - 1
            if (retry >= backoffMillis.size) return SmsSendResult.Failed(attempts)
            sleeper(backoffMillis[retry], stop)
        }
    }

    /** Stops the send in progress when [ack] is for its emergency; an ack for any other one is ignored. */
    fun onAck(ack: AckInfo) {
        if (sendingEmergencyId == ack.emergencyId) stop()
    }

    private fun emergencyIdOf(signed: ByteArray): Long? = try {
        PayloadCodec.decodeBody(signed.copyOfRange(0, PayloadFormat.BODY_SIZE)).emergencyId
    } catch (e: PayloadException) {
        null
    } catch (e: IllegalArgumentException) {
        null
    }

    /**
     * One attempt, raced against [stop] and [attemptTimeoutMillis]: true when the radio accepted
     * it, false when it failed or timed out, null when [stop] won (the attempt is cancelled).
     */
    private suspend fun attempt(to: String, text: String, stop: Deferred<Unit>): Boolean? = coroutineScope {
        val sending = async { withTimeoutOrNull(attemptTimeoutMillis) { sender.sendText(to, text) } ?: false }
        try {
            select<Boolean?> {
                sending.onAwait { it }
                stop.onAwait { null }
            }
        } finally {
            sending.cancel()
        }
    }

    /** Stops the send in progress (no-op when none): the attempt in flight is cut, no new one starts, the backoff ends now. */
    fun stop() {
        stopSignal.complete(Unit)
    }

    companion object {
        /** One SMS in GSM-7; ADR 0001 sizes the signed payload at 137 characters. */
        const val MAX_SMS_CHARS = 160

        /** Waits before retry 1, 2 and 3 after a failed attempt: three retries after the first try. */
        val backoffMillis: List<Long> = listOf(2_000L, 5_000L, 10_000L)

        /**
         * Longest wait for one sent status. The radio normally answers within seconds, so 30 s means
         * the broadcast was lost; the attempt then counts as failed and goes through the retries.
         */
        const val ATTEMPT_TIMEOUT_MILLIS = 30_000L

        private suspend fun sleepUnlessStopped(millis: Long, stop: Deferred<Unit>) {
            withTimeoutOrNull(millis) { stop.await() }
        }
    }
}
