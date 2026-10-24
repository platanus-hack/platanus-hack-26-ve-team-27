package com.team27.mama.emergency.sms

import com.team27.mama.emergency.payload.AckInfo
import com.team27.mama.emergency.payload.MamaText
import com.team27.mama.emergency.payload.Vectors
import com.team27.mama.emergency.payload.unhex
import org.junit.Assert.assertEquals
import org.junit.Assert.assertNull
import org.junit.Assert.assertTrue
import org.junit.Test

class SmsAckHandlerTest {
    private val ack = Vectors.file.acks.first()
    private val expected = AckInfo(java.lang.Long.parseUnsignedLong(ack.emergencyId, 16), ack.seq)
    private val serverKey = Vectors.file.keys.serverEd25519Public.unhex()

    private val published = mutableListOf<AckInfo>()
    private val acks = OfflineAcks().also { it.subscribe { a -> published += a } }
    private fun handler(key: ByteArray? = serverKey) = SmsAckHandler({ key }, acks)

    @Test fun validAckIsPublished() {
        assertEquals(expected, handler().handle(ack.sms))
        assertEquals(listOf(expected), published)
    }

    @Test fun badSignatureIsIgnored() {
        val bytes = ack.ack.unhex()
        bytes[bytes.size - 1] = (bytes[bytes.size - 1].toInt() xor 1).toByte()
        assertNull(handler().handle(MamaText.sms(bytes)))
        assertTrue(published.isEmpty())
    }

    @Test fun ackSignedByAnotherKeyIsIgnored() {
        assertNull(handler(Vectors.file.keys.deviceEd25519Public.unhex()).handle(ack.sms))
        assertTrue(published.isEmpty())
    }

    @Test fun noServerKeyIgnoresEverything() {
        assertNull(handler(key = null).handle(ack.sms))
        assertTrue(published.isEmpty())
    }

    @Test fun nonMamaTextsAreIgnored() {
        val h = handler()
        listOf("", "hola mamá", "MAMA1 !!!", "MAMA1 ", ack.mesh, ack.sms.removePrefix("MAMA1 "), ack.sms.dropLast(3))
            .forEach { assertNull(it, h.handle(it)) }
        assertTrue(published.isEmpty())
    }

    @Test fun anEmergencyPayloadIsNotAnAck() {
        assertNull(handler().handle(Vectors.file.payloads.first().sms))
        assertTrue(published.isEmpty())
    }

    @Test fun multipartBodyIsJoinedPerAddress() {
        val cut = ack.sms.length / 2
        val parts = listOf(
            SmsPart("+1555", ack.sms.substring(0, cut)),
            SmsPart("+1999", "unrelated"),
            SmsPart("+1555", ack.sms.substring(cut)),
        )
        assertEquals(listOf(expected), handler().handleParts(parts))
        assertEquals(listOf(expected), published)
    }

    @Test fun partsFromDifferentAddressesAreNotMixed() {
        val cut = ack.sms.length / 2
        val parts = listOf(SmsPart("+1555", ack.sms.substring(0, cut)), SmsPart("+1999", ack.sms.substring(cut)))
        assertTrue(handler().handleParts(parts).isEmpty())
        assertTrue(published.isEmpty())
    }

    @Test fun nullBodiesAreHarmless() {
        assertTrue(handler().handleParts(listOf(SmsPart(null, null))).isEmpty())
    }

    @Test fun unsubscribeStopsDelivery() {
        val hub = OfflineAcks()
        val seen = mutableListOf<AckInfo>()
        val off = hub.subscribe { seen += it }
        hub.publish(expected)
        off()
        hub.publish(expected)
        assertEquals(listOf(expected), seen)
    }
}
