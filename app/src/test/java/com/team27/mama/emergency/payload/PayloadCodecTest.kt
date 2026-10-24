package com.team27.mama.emergency.payload

import java.security.SecureRandom
import org.junit.Assert.assertEquals
import org.junit.Assert.assertNotEquals
import org.junit.Assert.assertThrows
import org.junit.Assert.fail
import org.junit.Test

class PayloadCodecTest {
    private fun ok() = EmergencyPayload(Kind.ALERT, 1L, 2L, 0, 1, 1, 5, 1000L)

    @Test
    fun bodyLayout() {
        val p = EmergencyPayload(
            Kind.ALERT, 0x0102030405060708L, 0x1112131415161718L, 0x2122,
            -1, 0x00313233, 0x4142, 0x51525354L,
        )
        val want = "0101" + "0102030405060708" + "1112131415161718" + "2122" +
            "ffffffff" + "00313233" + "4142" + "51525354"
        assertEquals(want, PayloadCodec.encodeBody(p).hex())
        assertEquals(p, PayloadCodec.decodeBody(want.unhex()))
    }

    @Test
    fun bodyRoundTripHighBits() {
        val p = EmergencyPayload(
            Kind.CANCEL, 0xfedcba9876543210uL.toLong(), 0xfedcba9876543210uL.toLong(),
            65535, -9_000_000, 18_000_000, 65534, 4294967295L,
        )
        assertEquals(p, PayloadCodec.decodeBody(PayloadCodec.encodeBody(p)))
    }

    @Test
    fun invalidPayloadIsRejectedAtConstruction() {
        val b = ok()
        assertThrows(IllegalArgumentException::class.java) { b.copy(latE5 = 9_000_001) }
        assertThrows(IllegalArgumentException::class.java) { b.copy(lonE5 = -18_000_001) }
        assertThrows(IllegalArgumentException::class.java) { b.copy(seq = 65536) }
        assertThrows(IllegalArgumentException::class.java) { b.copy(locAgeS = 65535, latE5 = 1) }
        assertThrows(IllegalArgumentException::class.java) { b.copy(timestamp = -1) }
    }

    @Test
    fun decodersRejectEveryLength() {
        val rnd = java.util.Random(54)
        val inputs = (0..120).map { ByteArray(it).also(rnd::nextBytes) } +
            (1..200).map { ByteArray(it % 121).also(rnd::nextBytes) }
        val key = ByteArray(32) { 7 }
        for (b in inputs) {
            val calls = listOf<() -> Any>(
                { PayloadCodec.decodeBody(b) },
                { PayloadCodec.verify(b, key) },
                { AckCodec.verify(b, key) },
            )
            for (f in calls) {
                try {
                    f()
                    fail("accepted ${b.size} bytes")
                } catch (e: PayloadException) {
                    // expected
                }
            }
        }
    }

    @Test
    fun degreesToE5RoundsHalfAwayFromZero() {
        assertEquals(1048801, PayloadCodec.degreesToE5(10.48801))
        assertEquals(-6687919, PayloadCodec.degreesToE5(-66.87919))
        assertEquals(3L, roundHalfAwayFromZero(2.5))
        assertEquals(-3L, roundHalfAwayFromZero(-2.5))
        assertEquals(1L, roundHalfAwayFromZero(0.5))
        assertEquals(-1L, roundHalfAwayFromZero(-0.5))
        assertEquals(0L, roundHalfAwayFromZero(-0.4))
        assertThrows(IllegalArgumentException::class.java) { PayloadCodec.degreesToE5(Double.NaN) }
        assertThrows(IllegalArgumentException::class.java) { PayloadCodec.degreesToE5(181.0) }
    }

    private class FixedSigner(val pub: ByteArray, val len: Int) : PayloadSigner {
        override val publicKey get() = pub
        override fun sign(message: ByteArray) = ByteArray(len)
    }

    @Test
    fun signRejectsSignerForOtherDevice() {
        val s = Ed25519SeedSigner(ByteArray(32) { 1 })
        assertThrows(IllegalArgumentException::class.java) {
            PayloadCodec.sign(ok().copy(deviceId = PayloadCodec.deviceIdOf(s.publicKey) + 1), s)
        }
    }

    @Test
    fun signRejectsShortSignature() {
        val pub = Ed25519SeedSigner(ByteArray(32) { 1 }).publicKey
        val s = FixedSigner(pub, 63)
        assertThrows(IllegalStateException::class.java) {
            PayloadCodec.sign(ok().copy(deviceId = PayloadCodec.deviceIdOf(pub)), s)
        }
    }

    @Test
    fun verifyRejectsWrongKeyLength() {
        val e = assertThrows(PayloadException::class.java) {
            PayloadCodec.verify(ByteArray(PayloadFormat.SIGNED_SIZE), ByteArray(0))
        }
        assertEquals(PayloadException.Reason.SIGNATURE, e.reason)
    }

    @Test
    fun newEmergencyIdUsesRandom() {
        assertNotEquals(PayloadCodec.newEmergencyId(), PayloadCodec.newEmergencyId())
        val fixed = object : SecureRandom() {
            override fun nextBytes(bytes: ByteArray) {
                for (i in bytes.indices) bytes[i] = (i + 1).toByte()
            }
        }
        assertEquals(0x0102030405060708L, PayloadCodec.newEmergencyId(fixed))
    }
}
