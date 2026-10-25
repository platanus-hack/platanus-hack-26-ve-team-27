package com.team27.mama.emergency

import com.team27.mama.emergency.payload.AckCodec
import com.team27.mama.emergency.payload.AckInfo
import com.team27.mama.emergency.payload.Ed25519SeedSigner
import com.team27.mama.emergency.payload.Kind
import com.team27.mama.emergency.payload.MamaText
import com.team27.mama.emergency.payload.MeshFrame
import com.team27.mama.emergency.payload.PayloadCodec
import com.team27.mama.emergency.payload.SealedBox
import org.junit.Assert.assertArrayEquals
import org.junit.Assert.assertEquals
import org.junit.Assert.assertNull
import org.junit.Assert.assertTrue
import org.junit.Test

class EmergencyFramesTest {
    private val device = Ed25519SeedSigner(ByteArray(32) { 1 })
    private val server = Ed25519SeedSigner(ByteArray(32) { 7 })
    private val serverXPrivate = ByteArray(32) { 9 }
    private val serverX = SealedBox.publicKeyOf(serverXPrivate)
    private val now = 1_790_000_000_000L

    private fun build(fix: LocationFix?) =
        EmergencyFrames.build(Kind.ALERT, device, 5L, 0, fix, now)

    @Test
    fun alertWithFix() {
        val frame = build(LocationFix(-33.45, -70.66, now - 12_345))
        val p = frame.payload
        assertEquals(Kind.ALERT, p.kind)
        assertEquals(0, p.seq)
        assertEquals(-3345000, p.latE5)
        assertEquals(-7066000, p.lonE5)
        assertEquals(12, p.locAgeS)
        assertEquals(now / 1000, p.timestamp)
        assertEquals(PayloadCodec.deviceIdOf(device.publicKey), p.deviceId)
        assertEquals(p, PayloadCodec.verify(frame.signed, device.publicKey))
    }

    @Test
    fun noFixIsNoLocation() {
        val p = build(null).payload
        assertEquals(65535, p.locAgeS)
        assertEquals(0, p.latE5)
        assertEquals(0, p.lonE5)
    }

    @Test
    fun oldFixSaturates() {
        assertEquals(65534, build(LocationFix(1.0, 1.0, now - 30L * 24 * 3600 * 1000)).payload.locAgeS)
        assertEquals(0, build(LocationFix(1.0, 1.0, now + 2_000)).payload.locAgeS)
    }

    @Test
    fun smsAndMeshTexts() {
        val frame = build(null)
        assertTrue(frame.smsText.startsWith("MAMA1 "))
        assertEquals(137, frame.smsText.length)
        assertArrayEquals(frame.signed, MamaText.parseSms(frame.smsText))
        val mesh = frame.meshText(serverX)
        assertTrue(mesh.startsWith("MAMA1:SEAL:"))
        assertEquals(207, mesh.length)
        val box = (MamaText.parseMesh(mesh) as MeshFrame.Sealed).box
        assertArrayEquals(frame.signed, SealedBox.open(box, serverXPrivate))
    }

    @Test
    fun ackReader() {
        val key = server.publicKey
        val ack = AckCodec.sign(AckInfo(42L, 1), server)
        val expected = AckInfo(42L, 1)
        assertEquals(expected, AckReader.fromMesh(MamaText.meshAck(ack), key))
        assertEquals(expected, AckReader.fromSms(MamaText.sms(ack), key))
        assertNull(AckReader.fromMesh(MamaText.meshAck(ack), null))
        assertNull(AckReader.fromSms(MamaText.sms(ack), null))
        val forged = AckCodec.sign(AckInfo(42L, 1), device)
        assertNull(AckReader.fromMesh(MamaText.meshAck(forged), key))
        assertNull(AckReader.fromSms(MamaText.sms(forged), key))
        assertNull(AckReader.fromMesh(build(null).meshText(serverX), key))
        assertNull(AckReader.fromMesh("hola", key))
        assertEquals(expected, AckReader.fromHttp(MamaText.meshAck(ack), null))
        assertEquals(expected, AckReader.fromHttp(MamaText.meshAck(ack), key))
        val badKind = ack.copyOf().also { it[1] = 3 }
        assertNull(AckReader.fromHttp(MamaText.meshAck(badKind), null))
        assertNull(AckReader.fromHttp("{", key))
        assertNull(AckReader.fromHttp("{", null))
    }
}
