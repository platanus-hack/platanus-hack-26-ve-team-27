package com.team27.mama.emergency

import com.team27.mama.emergency.EmergencyTestKit.Companion.decodeSms
import com.team27.mama.emergency.EmergencyTestKit.Companion.meshAck
import com.team27.mama.emergency.EmergencyTestKit.Companion.smsAck
import com.team27.mama.emergency.payload.AckInfo
import com.team27.mama.emergency.payload.Kind
import com.team27.mama.emergency.sms.FakeScheduler
import com.team27.mama.emergency.sms.FakeSender
import com.team27.mama.emergency.sms.SmsAckBus
import com.team27.mama.emergency.sms.SmsChannel
import com.team27.mama.emergency.sms.SmsChannelAdapter
import kotlinx.coroutines.ExperimentalCoroutinesApi
import kotlinx.coroutines.test.advanceTimeBy
import kotlinx.coroutines.test.runCurrent
import kotlinx.coroutines.test.runTest
import org.junit.Assert.assertEquals
import org.junit.Test

/** The orchestrator over #58's real [SmsChannel], through [SmsChannelAdapter]. */
@OptIn(ExperimentalCoroutinesApi::class)
class SmsAdapterWiringTest {
    private val scheduler = FakeScheduler()
    private val sender = FakeSender(scheduler)
    private val bus = SmsAckBus()

    private fun EmergencyTestKit.adapter() = SmsChannelAdapter(
        SmsChannel(sender, { config.smsNumber }, scheduler, bus),
        bus,
        { config.smsNumber },
        { true },
    )

    private fun kinds() = sender.sent.map { decodeSms(it.text).kind }

    @Test
    fun oneSmsEmissionPerFrameAndTheVerifiedSmsAckNotifies() = runTest {
        with(EmergencyTestKit()) {
            mesh.startResult = false
            val o = orchestrator(sms = adapter())
            o.declare()
            runCurrent()
            // The orchestrator ticks for five minutes; #58's own schedule has not moved yet.
            advanceTimeBy(300_000)
            assertEquals(listOf(Kind.ALERT), kinds())
            assertEquals(EmergencyPhase.SEARCHING_NEARBY, o.state.value.phase)

            bus.post(AckInfo(42L, 0), smsAck(42L, 0))
            runCurrent()
            assertEquals(EmergencyPhase.NOTIFIED, o.state.value.phase)
            assertEquals(0, scheduler.pendingCount)
            scheduler.advanceBy(1_000_000)
            assertEquals(1, sender.sent.size)
        }
    }

    @Test
    fun anAckOnAnotherChannelStopsTheSmsEmission() = runTest {
        with(EmergencyTestKit()) {
            val o = orchestrator(sms = adapter())
            o.declare()
            runCurrent()
            mesh.incoming.emit(meshAck(42L, 0))
            runCurrent()
            assertEquals(EmergencyPhase.NOTIFIED, o.state.value.phase)
            scheduler.advanceBy(1_000_000)
            assertEquals(listOf(Kind.ALERT), kinds())
        }
    }

    @Test
    fun cancelReplacesTheAlertEmissionWithTheCancelFrame() = runTest {
        with(EmergencyTestKit()) {
            mesh.startResult = false
            val o = orchestrator(sms = adapter())
            o.declare()
            runCurrent()
            o.cancel()
            runCurrent()
            assertEquals(listOf(Kind.ALERT, Kind.CANCEL), kinds())
            scheduler.advanceBy(1_000_000)
            assertEquals(1, kinds().count { it == Kind.ALERT })
            assertEquals(4, kinds().count { it == Kind.CANCEL })
        }
    }
}
