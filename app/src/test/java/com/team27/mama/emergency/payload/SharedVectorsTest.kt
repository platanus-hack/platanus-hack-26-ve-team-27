package com.team27.mama.emergency.payload

import org.junit.Assert.assertArrayEquals
import org.junit.Assert.assertEquals
import org.junit.Assert.assertTrue
import org.junit.Assert.fail
import org.junit.Test

class SharedVectorsTest {
    private val v = Vectors.file
    private val deviceSigner = Ed25519SeedSigner(v.keys.deviceEd25519Seed.unhex())
    private val serverSigner = Ed25519SeedSigner(v.keys.serverEd25519Seed.unhex())
    private val devicePublic = v.keys.deviceEd25519Public.unhex()
    private val serverPublic = v.keys.serverEd25519Public.unhex()

    private fun id(h: String): Long = java.lang.Long.parseUnsignedLong(h, 16)

    @Test
    fun keysMatch() {
        assertArrayEquals(devicePublic, deviceSigner.publicKey)
        assertArrayEquals(serverPublic, serverSigner.publicKey)
        assertEquals(id(v.keys.deviceId), PayloadCodec.deviceIdOf(devicePublic))
    }

    @Test
    fun degrees() {
        assertTrue(v.degrees.isNotEmpty())
        for (d in v.degrees) assertEquals("${d.deg}", d.e5, PayloadCodec.degreesToE5(d.deg))
    }

    @Test
    fun payloadBodiesAndSignatures() {
        assertTrue(v.payloads.isNotEmpty())
        for (p in v.payloads) {
            val fields = EmergencyPayload(
                kind = Kind.fromCode(p.kind)!!,
                deviceId = id(v.keys.deviceId),
                emergencyId = id(p.emergencyId),
                seq = p.seq,
                latE5 = p.latE5,
                lonE5 = p.lonE5,
                locAgeS = p.locAgeS,
                timestamp = p.timestamp,
            )
            assertEquals(p.name, p.body, PayloadCodec.encodeBody(fields).hex())
            assertEquals(p.name, p.signed, PayloadCodec.sign(fields, deviceSigner).hex())
            assertEquals(p.name, fields, PayloadCodec.verify(p.signed.unhex(), devicePublic))
        }
    }

    @Test
    fun acks() {
        assertTrue(v.acks.isNotEmpty())
        for (a in v.acks) {
            val info = AckInfo(id(a.emergencyId), a.seq)
            assertEquals(a.name, a.ack, AckCodec.sign(info, serverSigner).hex())
            assertEquals(a.name, info, AckCodec.verify(a.ack.unhex(), serverPublic))
        }
    }

    @Test
    fun invalidSignedHex() {
        val cases = v.invalid.filter { it.op == "signed_hex" }
        assertTrue(cases.isNotEmpty())
        for (c in cases) {
            try {
                PayloadCodec.verify(c.input.unhex(), devicePublic)
                fail("${c.name}: expected PayloadException")
            } catch (e: PayloadException) {
                assertEquals(c.name, c.error.uppercase(), e.reason.name)
            }
        }
    }
}
