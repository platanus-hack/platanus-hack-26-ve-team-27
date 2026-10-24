package com.team27.mama.emergency.sms

import com.team27.mama.emergency.config.EmergencyConfig
import com.team27.mama.emergency.payload.AckCodec
import com.team27.mama.emergency.payload.AckInfo
import com.team27.mama.emergency.payload.MamaText
import com.team27.mama.emergency.payload.PayloadException
import com.team27.mama.setup.PhoneNumbers
import java.util.concurrent.CopyOnWriteArrayList

/**
 * Turns an incoming SMS into a verified server ack, or null. Null for: a sender
 * that is not the central number (digits compared, formatting ignored), a body
 * that is not `MAMA1 ` + Base64url of an ack, a bad signature, or no server key
 * cached yet (before the first GET /config nothing can be verified).
 */
class SmsAckFilter(private val config: () -> EmergencyConfig) {
    fun parse(from: String?, body: String): AckInfo? {
        if (from.isNullOrBlank() || !body.trimStart().startsWith(MamaText.SMS_PREFIX)) return null
        val current = config()
        val serverKey = current.serverEd25519PublicKey ?: return null
        if (current.smsNumber.isBlank() || !PhoneNumbers.sameNumber(from, current.smsNumber)) return null
        return try {
            AckCodec.verify(MamaText.parseSms(body), serverKey)
        } catch (e: PayloadException) {
            null
        } catch (e: IllegalArgumentException) {
            null
        }
    }
}

/**
 * In-process hand-off from [SmsAckReceiver] to [SmsChannel] ([subscribe], the [AckInfo]) and to
 * #59's [SmsChannelAdapter] ([subscribeTexts], the verified body). Listeners run on the poster's thread.
 */
class SmsAckBus {
    private val listeners = CopyOnWriteArrayList<(AckInfo) -> Unit>()
    private val textListeners = CopyOnWriteArrayList<(String) -> Unit>()

    fun subscribe(listener: (AckInfo) -> Unit): Cancellable {
        listeners += listener
        return Cancellable { listeners -= listener }
    }

    /**
     * The SMS body of each verified ack, for #59's orchestrator ([SmsChannelAdapter]), which
     * reads acks from text and verifies them again. Only posts that carry the text reach it.
     */
    fun subscribeTexts(listener: (String) -> Unit): Cancellable {
        textListeners += listener
        return Cancellable { textListeners -= listener }
    }

    /** [ack] verified by [SmsAckFilter]; [text] is the body it was read from. */
    fun post(ack: AckInfo, text: String? = null) {
        listeners.forEach { it(ack) }
        if (text != null) textListeners.forEach { it(text) }
    }
}
