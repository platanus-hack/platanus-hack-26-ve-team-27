package com.team27.mama.emergency.sms

import com.team27.mama.emergency.EmergencyFrames
import com.team27.mama.emergency.EmergencyTestKit.Companion.DEVICE
import com.team27.mama.emergency.EmergencyTestKit.Companion.SERVER
import com.team27.mama.emergency.EmergencyTestKit.Companion.SMS_NUMBER
import com.team27.mama.emergency.payload.AckCodec
import com.team27.mama.emergency.payload.AckInfo
import com.team27.mama.emergency.payload.Kind
import com.team27.mama.emergency.payload.MamaText
import kotlinx.coroutines.ExperimentalCoroutinesApi
import kotlinx.coroutines.async
import kotlinx.coroutines.flow.toList
import kotlinx.coroutines.launch
import kotlinx.coroutines.test.runCurrent
import kotlinx.coroutines.test.runTest
import org.junit.Assert.assertEquals
import org.junit.Assert.assertFalse
import org.junit.Assert.assertTrue
import org.junit.Test

@OptIn(ExperimentalCoroutinesApi::class)
class SmsChannelAdapterTest {
    private val scheduler = FakeScheduler()
    private val bus = SmsAckBus()
    private var number = SMS_NUMBER
    private var granted = true

    private fun frame(seq: Int, kind: Kind = Kind.ALERT) =
        EmergencyFrames.build(kind, DEVICE, 42L, seq, null, 1_790_000_000_000L).smsText

    private fun adapter(sender: SmsSender, channel: Boolean = true): SmsChannelAdapter {
        val sms = if (channel) SmsChannel(sender, { number }, scheduler, bus, listOf(10L, 30L, 90L), 200L) else null
        return SmsChannelAdapter(sms, if (channel) bus else null, { number }, { granted })
    }

    @Test
    fun canSendNeedsTheChannelANumberAndThePermission() {
        val sender = FakeSender(scheduler)
        assertTrue(adapter(sender).canSend())
        assertFalse(adapter(sender, channel = false).canSend())
        number = "  "
        assertFalse(adapter(sender).canSend())
        number = SMS_NUMBER
        granted = false
        assertFalse(adapter(sender).canSend())
    }

    @Test
    fun flagOffSendsNothingAndHearsNothing() = runTest {
        val a = adapter(FakeSender(scheduler), channel = false)
        assertFalse(a.send(SMS_NUMBER, frame(0)))
        assertTrue(a.incoming.toList().isEmpty())
    }

    @Test
    fun sendSuspendsUntilTheEmissionEndsAndIsTrueWhenOneWentOut() = runTest {
        val sender = FakeSender(scheduler, results = listOf(false, true, false, false))
        val a = adapter(sender)
        val sent = async { a.send(SMS_NUMBER, frame(0)) }
        runCurrent()
        assertFalse(sent.isCompleted)
        assertEquals(frame(0), sender.sent.single().text)
        scheduler.advanceBy(10_000)
        assertTrue(sent.await())
        assertEquals(4, sender.sent.size)
    }

    @Test
    fun nothingWentOutIsFalse() = runTest {
        val a = adapter(FakeSender(scheduler, results = List(4) { false }))
        val sent = async { a.send(SMS_NUMBER, frame(0)) }
        runCurrent()
        scheduler.advanceBy(10_000)
        assertFalse(sent.await())
    }

    @Test
    fun blankNumberAtEmitIsFalse() = runTest {
        number = ""
        val sender = FakeSender(scheduler)
        assertFalse(adapter(sender).send(SMS_NUMBER, frame(0)))
        assertTrue(sender.sent.isEmpty())
    }

    /** #58 owns the retry schedule: the same frame again must not restart it. */
    @Test
    fun sameFrameWhileInFlightJoinsTheEmission() = runTest {
        val sender = FakeSender(scheduler)
        val a = adapter(sender)
        val first = async { a.send(SMS_NUMBER, frame(0)) }
        runCurrent()
        scheduler.advanceBy(5)
        val second = async { a.send(SMS_NUMBER, frame(0)) }
        runCurrent()
        assertEquals(listOf(0L), sender.sent.map { it.at })
        scheduler.advanceBy(10_000)
        assertEquals(listOf(0L, 10L, 40L, 130L), sender.sent.map { it.at })
        assertTrue(first.await())
        assertTrue(second.await())
    }

    @Test
    fun aNewFrameSupersedesTheOldOne() = runTest {
        val sender = FakeSender(scheduler)
        val a = adapter(sender)
        val alert = async { a.send(SMS_NUMBER, frame(0)) }
        runCurrent()
        val cancel = async { a.send(SMS_NUMBER, frame(1, Kind.CANCEL)) }
        runCurrent()
        assertFalse(alert.await())
        assertEquals(listOf(frame(0), frame(1, Kind.CANCEL)), sender.sent.map { it.text })
        scheduler.advanceBy(10_000)
        assertTrue(cancel.await())
    }

    @Test
    fun cancellingTheSendCancelsTheEmission() = runTest {
        val sender = FakeSender(scheduler)
        val a = adapter(sender)
        val job = launch { a.send(SMS_NUMBER, frame(0)) }
        runCurrent()
        job.cancel()
        runCurrent()
        scheduler.advanceBy(10_000)
        assertEquals(1, sender.sent.size)
        assertEquals(0, scheduler.pendingCount)
    }

    @Test
    fun theEmissionStopsOnlyWhenEveryWaiterIsCancelled() = runTest {
        val sender = FakeSender(scheduler)
        val a = adapter(sender)
        val one = launch { a.send(SMS_NUMBER, frame(0)) }
        val two = launch { a.send(SMS_NUMBER, frame(0)) }
        runCurrent()
        one.cancel()
        runCurrent()
        scheduler.advanceBy(15)
        assertEquals(2, sender.sent.size)
        two.cancel()
        runCurrent()
        scheduler.advanceBy(10_000)
        assertEquals(2, sender.sent.size)
        assertEquals(0, scheduler.pendingCount)
    }

    @Test
    fun verifiedAckTextsReachIncomingAndSmsChannelStillStopsOnTheAck() = runTest {
        val sender = FakeSender(scheduler)
        val a = adapter(sender)
        val heard = mutableListOf<String>()
        backgroundScope.launch { a.incoming.collect { heard += it } }
        val sent = async { a.send(SMS_NUMBER, frame(0)) }
        runCurrent()
        val text = MamaText.sms(AckCodec.sign(AckInfo(42L, 0), SERVER))
        bus.post(AckInfo(42L, 0), text)
        runCurrent()
        assertEquals(listOf(text), heard)
        assertTrue(sent.await())
        scheduler.advanceBy(10_000)
        assertEquals(1, sender.sent.size)
    }

    @Test
    fun anAckPostedWithoutTextIsNotForwarded() = runTest {
        val a = adapter(FakeSender(scheduler))
        val heard = mutableListOf<String>()
        backgroundScope.launch { a.incoming.collect { heard += it } }
        runCurrent()
        bus.post(AckInfo(42L, 0))
        runCurrent()
        assertTrue(heard.isEmpty())
    }
}
