package com.team27.mama.emergency.sms

import com.team27.mama.emergency.config.EmergencyConfig
import com.team27.mama.emergency.payload.AckCodec
import com.team27.mama.emergency.payload.AckInfo
import com.team27.mama.emergency.payload.Ed25519SeedSigner
import com.team27.mama.emergency.payload.MamaText
import com.team27.mama.emergency.payload.Vectors
import com.team27.mama.emergency.payload.unhex
import org.junit.Assert.assertEquals
import org.junit.Assert.assertNull
import org.junit.Test

class SmsAckFilterTest {
    private val server = Ed25519SeedSigner(Vectors.file.keys.serverEd25519Seed.unhex())
    private val impostor = Ed25519SeedSigner(ByteArray(32) { 9 })
    private val central = "+15555550100"
    private val ack = AckInfo(emergencyId = 0x0102030405060708L, seq = 3)
    private val ackSms = MamaText.sms(AckCodec.sign(ack, server))

    private fun filter(number: String = central, key: ByteArray? = server.publicKey) =
        SmsAckFilter { EmergencyConfig(number, key, ByteArray(32)) }

    @Test
    fun verifiedAckFromTheCentralNumber() {
        assertEquals(108, ackSms.length)
        assertEquals(ack, filter().parse(central, ackSms))
    }

    @Test
    fun senderFormattingIsIgnored() {
        assertEquals(ack, filter().parse("1 (555) 555-0100", ackSms))
        assertEquals(ack, filter(number = "+1 555-555-0100").parse("+15555550100", ackSms + "\n"))
    }

    @Test
    fun sharedVectorAcksVerify() {
        for (a in Vectors.file.acks) {
            assertEquals(a.name, a.seq, filter().parse(central, a.sms)?.seq)
        }
    }

    @Test
    fun otherSenderIsIgnored() {
        assertNull(filter().parse("+15555550199", ackSms))
        assertNull(filter().parse(null, ackSms))
        assertNull(filter().parse("", ackSms))
        assertNull(filter(number = "").parse(central, ackSms))
    }

    @Test
    fun badSignatureIsIgnored() {
        assertNull(filter().parse(central, MamaText.sms(AckCodec.sign(ack, impostor))))
        val tampered = AckCodec.sign(ack, server).also { it[5] = (it[5] + 1).toByte() }
        assertNull(filter().parse(central, MamaText.sms(tampered)))
    }

    @Test
    fun unknownServerKeyIgnoresEveryAck() {
        assertNull(filter(key = null).parse(central, ackSms))
    }

    @Test
    fun nonAckTextsAreIgnored() {
        assertNull(filter().parse(central, "Hola, ¿cómo estás?"))
        assertNull(filter().parse(central, "MAMA1 not-base64!"))
        assertNull(filter().parse(central, MamaText.sms(Vectors.file.payloads.first().signed.unhex())))
    }
}
