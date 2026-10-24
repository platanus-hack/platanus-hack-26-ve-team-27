package com.team27.mama.emergency

import com.team27.mama.emergency.payload.AckCodec
import com.team27.mama.emergency.payload.AckInfo
import com.team27.mama.emergency.payload.EmergencyPayload
import com.team27.mama.emergency.payload.Kind
import com.team27.mama.emergency.payload.MamaText
import com.team27.mama.emergency.payload.MeshFrame
import com.team27.mama.emergency.payload.PayloadCodec
import com.team27.mama.emergency.payload.PayloadException
import com.team27.mama.emergency.payload.PayloadFormat
import com.team27.mama.emergency.payload.PayloadSigner
import com.team27.mama.emergency.payload.SealedBox
import java.nio.ByteBuffer

/** A GPS fix: degrees and the wall-clock time (epoch ms) it was taken. */
data class LocationFix(val latDeg: Double, val lonDeg: Double, val timeMs: Long)

/** One signed emergency message, ready to be sent over SMS or the mesh. */
class Frame(val payload: EmergencyPayload, val signed: ByteArray) {
    val smsText: String get() = MamaText.sms(signed)

    fun meshText(serverX25519: ByteArray): String =
        MamaText.meshSealed(SealedBox.seal(signed, serverX25519))
}

object EmergencyFrames {
    fun build(
        kind: Kind,
        signer: PayloadSigner,
        emergencyId: Long,
        seq: Int,
        fix: LocationFix?,
        nowMs: Long,
    ): Frame {
        val payload = EmergencyPayload(
            kind = kind,
            deviceId = PayloadCodec.deviceIdOf(signer.publicKey),
            emergencyId = emergencyId,
            seq = seq,
            latE5 = fix?.let { PayloadCodec.degreesToE5(it.latDeg) } ?: 0,
            lonE5 = fix?.let { PayloadCodec.degreesToE5(it.lonDeg) } ?: 0,
            locAgeS = PayloadCodec.locAgeOf(fix?.let { (nowMs - it.timeMs) / 1000 }),
            timestamp = nowMs / 1000,
        )
        return Frame(payload, PayloadCodec.sign(payload, signer))
    }
}

/** Reads server acks from each channel. Never throws; anything unusable is `null`. */
object AckReader {
    fun fromMesh(text: String, serverEd25519: ByteArray?): AckInfo? {
        if (serverEd25519 == null) return null
        return try {
            val frame = MamaText.parseMesh(text) as? MeshFrame.Ack ?: return null
            AckCodec.verify(frame.ack, serverEd25519)
        } catch (_: PayloadException) {
            null
        }
    }

    fun fromSms(text: String, serverEd25519: ByteArray?): AckInfo? {
        if (serverEd25519 == null) return null
        return try {
            AckCodec.verify(MamaText.parseSms(text), serverEd25519)
        } catch (_: PayloadException) {
            null
        }
    }

    /** The HTTP reply carries the mesh ack text. Without a key it is decoded unverified. */
    fun fromHttp(text: String, serverEd25519: ByteArray?): AckInfo? {
        return try {
            val frame = MamaText.parseMesh(text) as? MeshFrame.Ack ?: return null
            if (serverEd25519 != null) return AckCodec.verify(frame.ack, serverEd25519)
            val bytes = frame.ack
            if (bytes.size != PayloadFormat.ACK_SIZE) return null
            if ((bytes[0].toInt() and 0xFF) != PayloadFormat.VERSION) return null
            if ((bytes[1].toInt() and 0xFF) != PayloadFormat.KIND_ACK) return null
            val b = ByteBuffer.wrap(bytes)
            AckInfo(b.getLong(2), b.getShort(10).toInt() and 0xFFFF)
        } catch (_: PayloadException) {
            null
        } catch (_: IllegalArgumentException) {
            null
        }
    }
}
