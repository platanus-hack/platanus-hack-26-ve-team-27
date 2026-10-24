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
    fun sealedMatchesGo() {
        val xPriv = v.keys.serverX25519Private.unhex()
        val xPub = v.keys.serverX25519Public.unhex()
        assertArrayEquals(xPub, SealedBox.publicKeyOf(xPriv))
        for (p in v.payloads) {
            val signed = p.signed.unhex()
            val sealed = SealedBox.sealWith(signed, xPub, p.ephemeralX25519Private.unhex())
            assertEquals(p.name, p.sealed, sealed.hex())
            assertArrayEquals(p.name, signed, SealedBox.open(p.sealed.unhex(), xPriv))
        }
    }

    @Test
    fun textsMatchGo() {
        for (p in v.payloads) {
            assertEquals(p.name, p.sms, MamaText.sms(p.signed.unhex()))
            assertArrayEquals(p.name, p.signed.unhex(), MamaText.parseSms(p.sms))
            assertEquals(p.name, p.mesh, MamaText.meshSealed(p.sealed.unhex()))
            assertArrayEquals(p.name, p.sealed.unhex(), (MamaText.parseMesh(p.mesh) as MeshFrame.Sealed).box)
        }
        for (a in v.acks) {
            assertEquals(a.name, a.sms, MamaText.sms(a.ack.unhex()))
            assertArrayEquals(a.name, a.ack.unhex(), MamaText.parseSms(a.sms))
            assertEquals(a.name, a.mesh, MamaText.meshAck(a.ack.unhex()))
            assertArrayEquals(a.name, a.ack.unhex(), (MamaText.parseMesh(a.mesh) as MeshFrame.Ack).ack)
        }
    }

    private fun formatError(msg: String): Nothing =
        throw PayloadException(PayloadException.Reason.FORMAT, msg)

    private fun runInvalid(c: VectorInvalid) {
        val xPriv = v.keys.serverX25519Private.unhex()
        when (c.op) {
            "sms_payload" -> PayloadCodec.verify(MamaText.parseSms(c.input), devicePublic)
            "sms_ack" -> AckCodec.verify(MamaText.parseSms(c.input), serverPublic)
            "mesh_payload" -> {
                val f = MamaText.parseMesh(c.input) as? MeshFrame.Sealed ?: formatError("not a SEAL text")
                PayloadCodec.verify(SealedBox.open(f.box, xPriv), devicePublic)
            }
            "mesh_ack" -> {
                val f = MamaText.parseMesh(c.input) as? MeshFrame.Ack ?: formatError("not an ACK text")
                AckCodec.verify(f.ack, serverPublic)
            }
            "signed_hex" -> PayloadCodec.verify(c.input.unhex(), devicePublic)
            else -> fail("unknown op ${c.op}")
        }
    }

    @Test
    fun invalidVectors() {
        assertTrue(v.invalid.isNotEmpty())
        for (c in v.invalid) {
            try {
                runInvalid(c)
                fail("${c.name}: expected PayloadException")
            } catch (e: PayloadException) {
                assertEquals(c.name, c.error.uppercase(), e.reason.name)
            }
        }
    }
}
