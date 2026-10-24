package com.team27.mama.emergency.sms

import com.team27.mama.emergency.payload.AckInfo
import com.team27.mama.emergency.payload.MamaText
import com.team27.mama.emergency.payload.Vectors
import com.team27.mama.emergency.payload.unhex
import org.junit.Assert.assertEquals
import org.junit.Assert.assertNull
import org.junit.Assert.assertTrue
import org.junit.Test

class SmsChannelTest {
    private val signed = Vectors.file.payloads.first().signed.unhex()
    private val scheduler = FakeScheduler()
    private val bus = SmsAckBus()
    private val outcomes = mutableListOf<SmsOutcome>()

    private fun channel(sender: SmsSender, number: String = "+15555550100") =
        SmsChannel(sender, { number }, scheduler, bus, retryDelaysMs = listOf(10L, 30L, 90L), ackWindowMs = 200L)

    private fun SmsChannel.start(emergencyId: Long = 7L, seq: Int = 2) =
        emit(signed, emergencyId, seq) { outcomes += it }

    @Test
    fun bodiesFromSharedVectorsFitOneGsmSms() {
        val safe = Regex("""^MAMA1 [A-Za-z0-9_-]+$""")
        for (p in Vectors.file.payloads) {
            val text = MamaText.sms(p.signed.unhex())
            assertEquals(p.name, p.sms, text)
            assertTrue(p.name, text.length <= SmsChannel.MAX_SMS_CHARS)
            assertTrue(p.name, safe.matches(text))
        }
    }

    @Test
    fun sendsTheVectorTextToTheCentralNumber() {
        val sender = FakeSender(scheduler)
        channel(sender, number = " +1 555 555 0100 ").start()
        assertEquals("+1 555 555 0100", sender.sent.single().number)
        assertEquals(Vectors.file.payloads.first().sms, sender.sent.single().text)
    }

    @Test
    fun retriesOnTheBackoffScheduleThenExhausts() {
        val sender = FakeSender(scheduler)
        val emission = channel(sender).start()
        scheduler.advanceBy(10_000)
        assertEquals(listOf(0L, 10L, 40L, 130L), sender.sent.map { it.at })
        assertEquals(listOf(10L, 30L, 90L, 200L), scheduler.requestedDelays)
        assertEquals(listOf<SmsOutcome>(SmsOutcome.Exhausted(attempts = 4, sent = 4)), outcomes)
        assertEquals(SmsOutcome.Exhausted(4, 4), emission.outcome)
        assertEquals(0, scheduler.pendingCount)
    }

    @Test
    fun exhaustsOnlyAfterTheLastAckWindow() {
        val sender = FakeSender(scheduler)
        channel(sender).start()
        scheduler.advanceBy(329)
        assertTrue(outcomes.isEmpty())
        scheduler.advanceBy(1)
        assertEquals(1, outcomes.size)
    }

    @Test
    fun failedSendUsesAnAttemptAndBacksOff() {
        val sender = FakeSender(scheduler, results = listOf(false, false, true, false))
        channel(sender).start()
        scheduler.advanceBy(10_000)
        assertEquals(listOf(0L, 10L, 40L, 130L), sender.sent.map { it.at })
        assertEquals(listOf<SmsOutcome>(SmsOutcome.Exhausted(attempts = 4, sent = 1)), outcomes)
    }

    @Test
    fun matchingAckStopsAtOnce() {
        val sender = FakeSender(scheduler)
        val emission = channel(sender).start(emergencyId = 7L, seq = 2)
        scheduler.advanceBy(15)
        bus.post(AckInfo(7L, 2))
        scheduler.advanceBy(10_000)
        assertEquals(2, sender.sent.size)
        assertEquals(listOf<SmsOutcome>(SmsOutcome.Acked(AckInfo(7L, 2))), outcomes)
        assertEquals(SmsOutcome.Acked(AckInfo(7L, 2)), emission.outcome)
        assertEquals(0, scheduler.pendingCount)
    }

    @Test
    fun higherSeqAckAlsoStops() {
        val sender = FakeSender(scheduler)
        channel(sender).start(emergencyId = 7L, seq = 2)
        bus.post(AckInfo(7L, 5))
        scheduler.advanceBy(10_000)
        assertEquals(1, sender.sent.size)
        assertEquals(listOf<SmsOutcome>(SmsOutcome.Acked(AckInfo(7L, 5))), outcomes)
    }

    @Test
    fun ackForAnotherEmergencyOrLowerSeqIsIgnored() {
        val sender = FakeSender(scheduler)
        val emission = channel(sender).start(emergencyId = 7L, seq = 2)
        bus.post(AckInfo(8L, 2))
        bus.post(AckInfo(7L, 1))
        assertNull(emission.outcome)
        scheduler.advanceBy(10_000)
        assertEquals(4, sender.sent.size)
        assertTrue(outcomes.single() is SmsOutcome.Exhausted)
    }

    @Test
    fun cancelStopsSending() {
        val sender = FakeSender(scheduler)
        val ch = channel(sender)
        ch.start()
        scheduler.advanceBy(15)
        ch.cancel()
        scheduler.advanceBy(10_000)
        assertEquals(2, sender.sent.size)
        assertEquals(listOf<SmsOutcome>(SmsOutcome.Cancelled), outcomes)
        assertEquals(0, scheduler.pendingCount)
    }

    @Test
    fun cancelOnTheHandleStopsSending() {
        val sender = FakeSender(scheduler)
        val emission = channel(sender).start()
        emission.cancel()
        emission.cancel()
        scheduler.advanceBy(10_000)
        assertEquals(1, sender.sent.size)
        assertEquals(listOf<SmsOutcome>(SmsOutcome.Cancelled), outcomes)
    }

    @Test
    fun blankNumberSendsNothing() {
        val sender = FakeSender(scheduler)
        val emission = channel(sender, number = "  ").start()
        scheduler.advanceBy(10_000)
        assertTrue(sender.sent.isEmpty())
        assertEquals(SmsOutcome.NoNumber, emission.outcome)
        assertEquals(listOf<SmsOutcome>(SmsOutcome.NoNumber), outcomes)
    }

    @Test
    fun newSeqSupersedesTheEmissionInFlight() {
        val sender = FakeSender(scheduler)
        val ch = channel(sender)
        val first = ch.start(emergencyId = 7L, seq = 2)
        scheduler.advanceBy(5)
        val second = ch.start(emergencyId = 7L, seq = 3)
        assertEquals(SmsOutcome.Superseded, first.outcome)
        // An ack for the old seq does not stop the new one.
        bus.post(AckInfo(7L, 2))
        assertNull(second.outcome)
        scheduler.advanceBy(10)
        assertEquals(listOf(0L, 5L, 15L), sender.sent.map { it.at })
        bus.post(AckInfo(7L, 3))
        scheduler.advanceBy(10_000)
        assertEquals(3, sender.sent.size)
        assertEquals(listOf(SmsOutcome.Superseded, SmsOutcome.Acked(AckInfo(7L, 3))), outcomes)
    }
}
