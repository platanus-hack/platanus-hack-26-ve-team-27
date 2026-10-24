package com.team27.mama.emergency.sms

import com.team27.mama.emergency.config.RemoteConfigRepository
import com.team27.mama.emergency.payload.MamaText
import kotlinx.coroutines.CompletableDeferred
import kotlinx.coroutines.Deferred
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

    /** [SmsChannel.stop] was called before an attempt succeeded. */
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
 * How the ack listener (slice 3) stops it: call [stop] when the server's ack SMS arrives.
 * Cancelling the coroutine that runs [send] works too. Either way no further attempt starts;
 * an SMS already handed to the radio cannot be recalled.
 *
 * One [send] at a time per channel: [stop] targets the send in progress.
 */
class SmsChannel(
    private val remoteConfig: RemoteConfigRepository,
    private val sender: SmsSender,
    /** Waits [millis] between attempts, returning early when [stop] completes. Injected for tests. */
    private val sleeper: suspend (millis: Long, stop: Deferred<Unit>) -> Unit = ::sleepUnlessStopped,
) {
    @Volatile
    private var stopSignal = CompletableDeferred<Unit>()

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
        var attempts = 0
        while (true) {
            if (stop.isCompleted) return SmsSendResult.Stopped(attempts)
            attempts++
            if (sender.sendText(to, text)) return SmsSendResult.Sent(attempts)
            val retry = attempts - 1
            if (retry >= backoffMillis.size) return SmsSendResult.Failed(attempts)
            sleeper(backoffMillis[retry], stop)
        }
    }

    /** Stops the send in progress (no-op when none): no new attempt starts and the backoff ends now. */
    fun stop() {
        stopSignal.complete(Unit)
    }

    companion object {
        /** One SMS in GSM-7; ADR 0001 sizes the signed payload at 137 characters. */
        const val MAX_SMS_CHARS = 160

        /** Waits before retry 1, 2 and 3 after a failed attempt: three retries after the first try. */
        val backoffMillis: List<Long> = listOf(2_000L, 5_000L, 10_000L)

        private suspend fun sleepUnlessStopped(millis: Long, stop: Deferred<Unit>) {
            withTimeoutOrNull(millis) { stop.await() }
        }
    }
}
