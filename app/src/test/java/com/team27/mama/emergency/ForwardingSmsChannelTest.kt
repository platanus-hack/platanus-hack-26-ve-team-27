package com.team27.mama.emergency

import com.team27.mama.emergency.EmergencyTestKit.Companion.SMS_NUMBER
import com.team27.mama.emergency.EmergencyTestKit.Companion.smsAck
import kotlinx.coroutines.ExperimentalCoroutinesApi
import kotlinx.coroutines.test.runCurrent
import kotlinx.coroutines.test.runTest
import org.junit.Assert.assertEquals
import org.junit.Assert.assertFalse
import org.junit.Assert.assertSame
import org.junit.Assert.assertTrue
import org.junit.Test

@OptIn(ExperimentalCoroutinesApi::class)
class ForwardingSmsChannelTest {
    @Test
    fun everyCallReadsTheCurrentChannel() = runTest {
        var current: SmsEmergencyChannel = NoSmsChannel
        val forwarding = ForwardingSmsChannel { current }
        assertFalse(forwarding.canSend())
        assertFalse(forwarding.send(SMS_NUMBER, "MAMA1 x"))
        assertSame(NoSmsChannel.incoming, forwarding.incoming)

        val real = FakeSms()
        current = real
        assertTrue(forwarding.canSend())
        assertTrue(forwarding.send(SMS_NUMBER, "MAMA1 x"))
        assertEquals(listOf(SMS_NUMBER to "MAMA1 x"), real.sent)
        assertSame(real.incoming, forwarding.incoming)
    }

    /** #58 sets the sender after `graph.emergency` was built: the next emergency uses it, acks included. */
    @Test
    fun senderSetAfterTheOrchestratorWasBuiltServesTheNextEmergency() = runTest {
        with(EmergencyTestKit()) {
            var current: SmsEmergencyChannel = NoSmsChannel
            mesh.startResult = false
            val o = orchestrator(sms = ForwardingSmsChannel { current })
            o.declare()
            o.cancel()
            runCurrent()
            assertEquals(EmergencyPhase.CANCELLED, o.state.value.phase)

            current = sms
            assertTrue(o.declare())
            runCurrent()
            assertEquals(1, sms.sent.size)
            sms.incoming.emit(smsAck(42, 0))
            runCurrent()
            assertEquals(EmergencyPhase.NOTIFIED, o.state.value.phase)
        }
    }
}
