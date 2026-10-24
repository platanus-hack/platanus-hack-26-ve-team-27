package com.team27.mama.emergency.payload

import java.util.Base64

/** A parsed mesh text: a sealed box or an ack. */
sealed class MeshFrame {
    class Sealed(val box: ByteArray) : MeshFrame()
    class Ack(val ack: ByteArray) : MeshFrame()
}

/** SMS and mesh transport texts, byte-identical to the Go package. */
object MamaText {
    const val SMS_PREFIX = "MAMA1 "
    const val MESH_PREFIX = "MAMA1:"
    const val MESH_SEALED_PREFIX = "MAMA1:SEAL:"
    const val MESH_ACK_PREFIX = "MAMA1:ACK:"

    private const val WHITESPACE = " \t\r\n"

    private val smsEncoder = Base64.getUrlEncoder().withoutPadding()
    private val smsDecoder = Base64.getUrlDecoder()
    private val meshEncoder = Base64.getEncoder()
    private val meshDecoder = Base64.getDecoder()

    fun sms(frame: ByteArray): String = SMS_PREFIX + smsEncoder.encodeToString(frame)

    fun meshSealed(box: ByteArray): String = MESH_SEALED_PREFIX + meshEncoder.encodeToString(box)

    fun meshAck(ack: ByteArray): String = MESH_ACK_PREFIX + meshEncoder.encodeToString(ack)

    fun parseSms(text: String): ByteArray {
        val s = text.trim { it in WHITESPACE }
        if (!s.startsWith(SMS_PREFIX)) throw format("missing \"$SMS_PREFIX\" prefix")
        return decodeCanonical(smsDecoder, smsEncoder, s.substring(SMS_PREFIX.length))
    }

    fun isMesh(text: String): Boolean = text.trim { it in WHITESPACE }.startsWith(MESH_PREFIX)

    fun parseMesh(text: String): MeshFrame {
        val s = text.trim { it in WHITESPACE }
        if (!s.startsWith(MESH_PREFIX)) throw format("missing \"$MESH_PREFIX\" prefix")
        val rest = s.substring(MESH_PREFIX.length)
        val colon = rest.indexOf(':')
        if (colon < 0) throw format("missing mesh tag")
        val tag = rest.substring(0, colon)
        val isSeal = when (tag) {
            "SEAL" -> true
            "ACK" -> false
            else -> throw format("unknown mesh tag \"$tag\"")
        }
        val bytes = decodeCanonical(meshDecoder, meshEncoder, rest.substring(colon + 1))
        return if (isSeal) MeshFrame.Sealed(bytes) else MeshFrame.Ack(bytes)
    }

    private fun format(msg: String) = PayloadException(PayloadException.Reason.FORMAT, msg)

    private fun decodeCanonical(
        decoder: Base64.Decoder,
        encoder: Base64.Encoder,
        s: String,
    ): ByteArray {
        if (s.isEmpty()) throw format("empty Base64")
        val bytes = try {
            decoder.decode(s)
        } catch (e: IllegalArgumentException) {
            throw PayloadException(PayloadException.Reason.FORMAT, "invalid Base64", e)
        }
        if (encoder.encodeToString(bytes) != s) throw format("Base64 is not canonical")
        return bytes
    }
}
