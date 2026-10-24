package com.team27.mama.emergency.sms

import com.team27.mama.emergency.payload.AckCodec
import com.team27.mama.emergency.payload.AckInfo
import com.team27.mama.emergency.payload.MamaText
import com.team27.mama.emergency.payload.PayloadException

/** One received SMS part, as [android.telephony.SmsMessage] reports it. */
data class SmsPart(val address: String?, val body: String?)

/**
 * The pure core of the ack listener (#58): from received SMS texts to a verified ack on [acks].
 * [SmsAckReceiver] is the thin Android shell around it.
 *
 * Anything that is not a `MAMA1 ` text carrying a 76-byte ack signed by the server is ignored
 * without a trace: nothing is logged, since the numbers and bodies are private. Without a cached
 * server key ([serverKey] returns null) nothing is accepted.
 */
class SmsAckHandler(
    private val serverKey: () -> ByteArray?,
    private val acks: OfflineAcks,
) {
    /**
     * Joins the parts of a multipart SMS per originating address (in arrival order) and handles
     * each text. Returns the acks that were published.
     */
    fun handleParts(parts: List<SmsPart>): List<AckInfo> =
        parts.groupBy { it.address.orEmpty() }
            .values
            .mapNotNull { group -> handle(group.joinToString("") { it.body.orEmpty() }) }

    /** Publishes and returns the ack when [text] is a valid server ack, else null. */
    fun handle(text: String): AckInfo? {
        if (!text.trimStart().startsWith(MamaText.SMS_PREFIX)) return null
        val key = serverKey() ?: return null
        val ack = try {
            AckCodec.verify(MamaText.parseSms(text), key)
        } catch (e: PayloadException) {
            return null
        } catch (e: IllegalArgumentException) {
            return null
        }
        acks.publish(ack)
        return ack
    }
}
