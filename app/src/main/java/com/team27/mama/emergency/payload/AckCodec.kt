package com.team27.mama.emergency.payload

import java.nio.ByteBuffer

/** Server acknowledgement: the highest [seq] accepted for [emergencyId]. */
data class AckInfo(val emergencyId: Long, val seq: Int) {
    init {
        require(seq in 0..65535) { "seq out of range: $seq" }
    }
}

object AckCodec {
    fun sign(ack: AckInfo, signer: PayloadSigner): ByteArray {
        val body = ByteBuffer.allocate(PayloadFormat.ACK_BODY_SIZE)
            .put(PayloadFormat.VERSION.toByte())
            .put(PayloadFormat.KIND_ACK.toByte())
            .putLong(ack.emergencyId)
            .putShort(ack.seq.toShort())
            .array()
        return signWithContext(body, signer)
    }

    fun verify(bytes: ByteArray, serverPublicKey: ByteArray): AckInfo {
        if (bytes.size != PayloadFormat.ACK_SIZE) {
            throw PayloadException(
                PayloadException.Reason.FORMAT,
                "ack must be ${PayloadFormat.ACK_SIZE} bytes, got ${bytes.size}",
            )
        }
        val b = ByteBuffer.wrap(bytes)
        val version = b.get().toInt() and 0xFF
        if (version != PayloadFormat.VERSION) {
            throw PayloadException(PayloadException.Reason.FORMAT, "unsupported version $version")
        }
        val kind = b.get().toInt() and 0xFF
        if (kind != PayloadFormat.KIND_ACK) {
            throw PayloadException(PayloadException.Reason.FORMAT, "not an ack: kind $kind")
        }
        requirePublicKeySize(serverPublicKey)
        val emergencyId = b.long
        val seq = b.short.toInt() and 0xFFFF
        val body = bytes.copyOfRange(0, PayloadFormat.ACK_BODY_SIZE)
        val sig = bytes.copyOfRange(PayloadFormat.ACK_BODY_SIZE, PayloadFormat.ACK_SIZE)
        if (!ed25519Verify(serverPublicKey, SIGNATURE_CONTEXT_BYTES + body, sig)) {
            throw PayloadException(PayloadException.Reason.SIGNATURE, "bad signature")
        }
        return AckInfo(emergencyId, seq)
    }
}
