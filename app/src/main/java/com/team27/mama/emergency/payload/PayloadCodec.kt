package com.team27.mama.emergency.payload

import java.math.BigDecimal
import java.math.RoundingMode
import java.nio.ByteBuffer
import java.security.MessageDigest
import java.security.SecureRandom
import org.bouncycastle.crypto.params.Ed25519PublicKeyParameters
import org.bouncycastle.crypto.signers.Ed25519Signer

internal val SIGNATURE_CONTEXT_BYTES: ByteArray = PayloadFormat.SIGNATURE_CONTEXT.toByteArray(Charsets.US_ASCII)

/** Round half away from zero on the exact value of the double. */
internal fun roundHalfAwayFromZero(x: Double): Long {
    require(x.isFinite()) { "not finite: $x" }
    return BigDecimal(x).setScale(0, RoundingMode.HALF_UP).toLong()
}

/** Pure Ed25519 verify; false on any malformed input. */
internal fun ed25519Verify(publicKey: ByteArray, message: ByteArray, signature: ByteArray): Boolean =
    try {
        val v = Ed25519Signer()
        v.init(false, Ed25519PublicKeyParameters(publicKey, 0))
        v.update(message, 0, message.size)
        v.verifySignature(signature)
    } catch (e: RuntimeException) {
        false
    }

object PayloadCodec {
    fun deviceIdOf(publicKey: ByteArray): Long =
        ByteBuffer.wrap(MessageDigest.getInstance("SHA-256").digest(publicKey), 0, 8).long

    fun degreesToE5(deg: Double): Int {
        require(deg.isFinite() && kotlin.math.abs(deg) <= 180.0) { "invalid degrees: $deg" }
        return roundHalfAwayFromZero(deg * 1e5).toInt()
    }

    fun newEmergencyId(random: SecureRandom = SecureRandom()): Long {
        val b = ByteArray(8)
        random.nextBytes(b)
        return ByteBuffer.wrap(b).long
    }

    fun encodeBody(p: EmergencyPayload): ByteArray =
        ByteBuffer.allocate(PayloadFormat.BODY_SIZE)
            .put(PayloadFormat.VERSION.toByte())
            .put(p.kind.code.toByte())
            .putLong(p.deviceId)
            .putLong(p.emergencyId)
            .putShort(p.seq.toShort())
            .putInt(p.latE5)
            .putInt(p.lonE5)
            .putShort(p.locAgeS.toShort())
            .putInt(p.timestamp.toInt())
            .array()

    fun decodeBody(bytes: ByteArray): EmergencyPayload {
        if (bytes.size != PayloadFormat.BODY_SIZE) {
            throw PayloadException(
                PayloadException.Reason.FORMAT,
                "body must be ${PayloadFormat.BODY_SIZE} bytes, got ${bytes.size}",
            )
        }
        val b = ByteBuffer.wrap(bytes)
        val version = b.get().toInt() and 0xFF
        if (version != PayloadFormat.VERSION) {
            throw PayloadException(PayloadException.Reason.FORMAT, "unsupported version $version")
        }
        val code = b.get().toInt() and 0xFF
        val kind = Kind.fromCode(code)
            ?: throw PayloadException(PayloadException.Reason.FORMAT, "invalid kind $code")
        return try {
            EmergencyPayload(
                kind = kind,
                deviceId = b.long,
                emergencyId = b.long,
                seq = b.short.toInt() and 0xFFFF,
                latE5 = b.int,
                lonE5 = b.int,
                locAgeS = b.short.toInt() and 0xFFFF,
                timestamp = b.int.toLong() and 0xFFFFFFFFL,
            )
        } catch (e: IllegalArgumentException) {
            throw PayloadException(PayloadException.Reason.FORMAT, e.message ?: "invalid body", e)
        }
    }

    fun sign(p: EmergencyPayload, signer: PayloadSigner): ByteArray {
        require(p.deviceId == deviceIdOf(signer.publicKey)) { "payload deviceId does not match signer key" }
        val body = encodeBody(p)
        val sig = signer.sign(SIGNATURE_CONTEXT_BYTES + body)
        check(sig.size == PayloadFormat.SIGNATURE_SIZE) { "signer returned ${sig.size} bytes" }
        return body + sig
    }

    fun verify(signed: ByteArray, publicKey: ByteArray): EmergencyPayload {
        if (signed.size != PayloadFormat.SIGNED_SIZE) {
            throw PayloadException(
                PayloadException.Reason.FORMAT,
                "signed payload must be ${PayloadFormat.SIGNED_SIZE} bytes, got ${signed.size}",
            )
        }
        val body = signed.copyOfRange(0, PayloadFormat.BODY_SIZE)
        val p = decodeBody(body)
        if (publicKey.size != 32) {
            throw PayloadException(PayloadException.Reason.SIGNATURE, "public key must be 32 bytes")
        }
        if (p.deviceId != deviceIdOf(publicKey)) {
            throw PayloadException(PayloadException.Reason.SIGNATURE, "device id does not match public key")
        }
        val sig = signed.copyOfRange(PayloadFormat.BODY_SIZE, PayloadFormat.SIGNED_SIZE)
        if (!ed25519Verify(publicKey, SIGNATURE_CONTEXT_BYTES + body, sig)) {
            throw PayloadException(PayloadException.Reason.SIGNATURE, "bad signature")
        }
        return p
    }
}
