package com.team27.mama.emergency.payload

import org.junit.Assert.assertArrayEquals
import org.junit.Assert.assertEquals
import org.junit.Assert.assertFalse
import org.junit.Assert.assertTrue
import org.junit.Assert.fail
import org.junit.Test

class MamaTextTest {
    private val signed = ByteArray(98) { (it * 7 + 1).toByte() }
    private val ack = ByteArray(76) { (it * 5 + 2).toByte() }
    private val box = ByteArray(146) { (it * 11 + 3).toByte() }

    private fun assertFormat(text: String, block: (String) -> Unit = { MamaText.parseSms(it) }) {
        try {
            block(text)
            fail("expected PayloadException for '$text'")
        } catch (e: PayloadException) {
            assertEquals(PayloadException.Reason.FORMAT, e.reason)
        }
    }

    @Test
    fun smsLengths() {
        assertEquals(137, MamaText.sms(signed).length)
        assertEquals(108, MamaText.sms(ack).length)
        assertFalse(MamaText.sms(signed).contains('='))
        assertArrayEquals(signed, MamaText.parseSms(MamaText.sms(signed)))
    }

    @Test
    fun parseSmsTrimsOuterWhitespace() {
        val t = MamaText.sms(signed)
        assertArrayEquals(signed, MamaText.parseSms("  " + t + "\r\n"))
    }

    @Test
    fun parseSmsRejects() {
        val t = MamaText.sms(signed)
        assertFormat(t + "=")
        assertFormat(t.substring(0, 60) + "\n" + t.substring(60))
        assertFormat("mama1 " + t.substring(6))
        assertFormat("MAMA1:" + t.substring(6))
        assertFormat(t.substring(0, 10) + "+" + t.substring(11))
        assertFormat("MAMA1 ")
        assertFormat("")
    }

    @Test
    fun meshTexts() {
        val s = MamaText.meshSealed(box)
        val a = MamaText.meshAck(ack)
        assertEquals(207, s.length)
        assertEquals(114, a.length)
        assertTrue(s.startsWith("MAMA1:SEAL:"))
        assertTrue(a.startsWith("MAMA1:ACK:"))
        assertArrayEquals(box, (MamaText.parseMesh(s) as MeshFrame.Sealed).box)
        assertArrayEquals(ack, (MamaText.parseMesh(a) as MeshFrame.Ack).ack)
        assertTrue(MamaText.isMesh(s))
        assertTrue(MamaText.isMesh(a))
        assertFalse(MamaText.isMesh("MAMA1 x"))
        assertFalse(MamaText.isMesh("hola"))
    }

    @Test
    fun parseMeshRejects() {
        val s = MamaText.meshSealed(box)
        val b64 = s.removePrefix("MAMA1:SEAL:")
        val parse = { x: String -> MamaText.parseMesh(x); Unit }
        assertFormat("MAMA1:XYZ:" + b64, parse)
        assertFormat("MAMA1:", parse)
        assertFormat("MAMA1:SEAL:" + b64.trimEnd('='), parse)
        assertFormat("MAMA1:SEAL:" + b64.replace('+', '-').replace('/', '_') + "-", parse)
        assertFormat("MAMA1:SEAL:" + b64.substring(0, 60) + "\n" + b64.substring(60), parse)
    }
}
