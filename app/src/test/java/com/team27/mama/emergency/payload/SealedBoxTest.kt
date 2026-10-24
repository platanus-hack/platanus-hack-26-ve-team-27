package com.team27.mama.emergency.payload

import java.security.SecureRandom
import kotlin.random.Random
import org.junit.Assert.assertArrayEquals
import org.junit.Assert.assertEquals
import org.junit.Assert.assertFalse
import org.junit.Assert.fail
import org.junit.Test

class SealedBoxTest {
    private val priv = ByteArray(32) { (it + 1).toByte() }
    private val pub = SealedBox.publicKeyOf(priv)

    private fun reason(block: () -> Unit): PayloadException.Reason {
        try {
            block()
        } catch (e: PayloadException) {
            return e.reason
        }
        fail("expected PayloadException")
        throw IllegalStateException()
    }

    @Test
    fun roundTrip() {
        val pt = ByteArray(98) { it.toByte() }
        val a = SealedBox.seal(pt, pub, SecureRandom())
        val b = SealedBox.seal(pt, pub, SecureRandom())
        assertEquals(146, a.size)
        assertArrayEquals(pt, SealedBox.open(a, priv))
        assertFalse(a.contentEquals(b))
    }

    @Test
    fun sealWithIsDeterministic() {
        val eph = ByteArray(32) { (it * 3 + 7).toByte() }
        val pt = ByteArray(98) { 5 }
        val a = SealedBox.sealWith(pt, pub, eph)
        assertArrayEquals(a, SealedBox.sealWith(pt, pub, eph))
        assertArrayEquals(SealedBox.publicKeyOf(eph), a.copyOfRange(0, 32))
    }

    @Test
    fun openRejectsShortAndGarbage() {
        for (n in 0..47) {
            assertEquals(PayloadException.Reason.FORMAT, reason { SealedBox.open(ByteArray(n), priv) })
        }
        val rnd = Random(54)
        repeat(200) {
            val box = rnd.nextBytes(rnd.nextInt(48, 201))
            assertEquals(PayloadException.Reason.DECRYPT, reason { SealedBox.open(box, priv) })
        }
    }

    @Test
    fun openRejectsTamperAndWrongKey() {
        val box = SealedBox.seal(ByteArray(98), pub)
        val t1 = box.copyOf().also { it[40] = (it[40].toInt() xor 1).toByte() }
        val t2 = box.copyOf().also { it[it.size - 1] = (it[it.size - 1].toInt() xor 1).toByte() }
        assertEquals(PayloadException.Reason.DECRYPT, reason { SealedBox.open(t1, priv) })
        assertEquals(PayloadException.Reason.DECRYPT, reason { SealedBox.open(t2, priv) })
        val other = ByteArray(32) { (it + 9).toByte() }
        assertEquals(PayloadException.Reason.DECRYPT, reason { SealedBox.open(box, other) })
    }

    @Test
    fun lowOrderEphemeralIsDecryptError() {
        val box = SealedBox.seal(ByteArray(98), pub)
        for (i in 0 until 32) box[i] = 0
        assertEquals(PayloadException.Reason.DECRYPT, reason { SealedBox.open(box, priv) })
    }
}
