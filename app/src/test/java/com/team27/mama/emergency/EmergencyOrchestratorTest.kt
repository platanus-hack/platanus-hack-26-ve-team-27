package com.team27.mama.emergency

import com.team27.mama.emergency.EmergencyTestKit.Companion.DEVICE
import com.team27.mama.emergency.EmergencyTestKit.Companion.NOW
import com.team27.mama.emergency.EmergencyTestKit.Companion.SERVER_X
import com.team27.mama.emergency.EmergencyTestKit.Companion.SMS_NUMBER
import com.team27.mama.emergency.EmergencyTestKit.Companion.decodeMesh
import com.team27.mama.emergency.EmergencyTestKit.Companion.decodeSms
import com.team27.mama.emergency.EmergencyTestKit.Companion.httpAck
import com.team27.mama.emergency.EmergencyTestKit.Companion.meshAck
import com.team27.mama.emergency.EmergencyTestKit.Companion.smsAck
import com.team27.mama.emergency.config.EmergencyConfig
import com.team27.mama.emergency.payload.AckCodec
import com.team27.mama.emergency.payload.AckInfo
import com.team27.mama.emergency.payload.Kind
import com.team27.mama.emergency.payload.MamaText
import com.team27.mama.emergency.payload.PayloadSigner
import com.team27.mama.server.ServerHttpException
import java.io.IOException
import kotlinx.coroutines.ExperimentalCoroutinesApi
import kotlinx.coroutines.awaitCancellation
import kotlinx.coroutines.test.TestScope
import kotlinx.coroutines.test.advanceTimeBy
import kotlinx.coroutines.test.runCurrent
import kotlinx.coroutines.test.runTest
import org.junit.Assert.assertEquals
import org.junit.Assert.assertFalse
import org.junit.Assert.assertNull
import org.junit.Assert.assertTrue
import org.junit.Test

@OptIn(ExperimentalCoroutinesApi::class)
class EmergencyOrchestratorTest {

    /** Advances virtual time to exactly [t] ms after the test started and runs what is due. */
    private fun TestScope.at(t: Long) {
        advanceTimeBy(t - testScheduler.currentTime)
        runCurrent()
    }

    @Test
    fun httpOkUsesNoOtherChannel() = runTest {
        with(EmergencyTestKit()) {
            online = true
            http.behaviour = { httpAck(42, 0) }
            val o = orchestrator()
            assertTrue(o.declare())
            runCurrent()
            val s = o.state.value
            assertEquals(EmergencyPhase.NOTIFIED, s.phase)
            assertEquals(42L, s.emergencyId)
            assertEquals(0, s.ackedSeq)
            assertEquals(setOf(Channel.HTTP), s.channelsUsed)
            val p = decodeSms(http.posted.single())
            assertEquals(Kind.ALERT, p.kind)
            assertEquals(0, p.seq)
            assertEquals(42L, p.emergencyId)
            advanceTimeBy(600_000)
            assertTrue(sms.sent.isEmpty())
            assertEquals(0, mesh.startCalls)
            assertTrue(mesh.broadcasts.isEmpty())
        }
    }

    @Test
    fun offlineSkipsHttpAndStartsSmsAndMeshTogether() = runTest {
        with(EmergencyTestKit()) {
            val o = orchestrator()
            o.declare()
            runCurrent()
            assertEquals(0L, testScheduler.currentTime)
            assertTrue(http.posted.isEmpty())
            val (number, text) = sms.sent.single()
            assertEquals(SMS_NUMBER, number)
            val broadcast = mesh.broadcasts.single()
            assertEquals(EmergencyPhase.SEARCHING_NEARBY, o.state.value.phase)
            assertEquals(setOf(Channel.SMS, Channel.MESH), o.state.value.channelsUsed)
            assertEquals(decodeSms(text), decodeMesh(broadcast))
        }
    }

    @Test
    fun httpFailureStartsSmsAndMeshInParallel() = runTest {
        with(EmergencyTestKit()) {
            online = true
            http.behaviour = { throw IOException("boom") }
            sms.behaviour = { _, _ -> awaitCancellation() }
            val o = orchestrator()
            o.declare()
            runCurrent()
            assertEquals(1, sms.sent.size)
            assertEquals(1, mesh.broadcasts.size)
            assertEquals(setOf(Channel.HTTP, Channel.SMS, Channel.MESH), o.state.value.channelsUsed)
            assertEquals(EmergencyPhase.SEARCHING_NEARBY, o.state.value.phase)
        }
    }

    @Test
    fun httpTimeoutStartsFallbackAtFiveSeconds() = runTest {
        with(EmergencyTestKit()) {
            online = true
            http.behaviour = { awaitCancellation() }
            val o = orchestrator()
            o.declare()
            at(4_999)
            assertTrue(sms.sent.isEmpty())
            assertTrue(mesh.broadcasts.isEmpty())
            assertEquals(EmergencyPhase.SENDING, o.state.value.phase)
            at(5_000)
            assertEquals(1, sms.sent.size)
            assertEquals(1, mesh.broadcasts.size)
            assertEquals(EmergencyPhase.SEARCHING_NEARBY, o.state.value.phase)
        }
    }

    @Test
    fun meshReemitsOnCadence() = runTest {
        with(EmergencyTestKit()) {
            val o = orchestrator()
            o.declare()
            runCurrent()
            assertEquals(1, mesh.broadcasts.size)
            val ticks = listOf(5_000L, 15_000L, 35_000L, 75_000L, 135_000L, 195_000L)
            ticks.forEachIndexed { i, t ->
                at(t - 1)
                assertEquals("just before $t", i + 1, mesh.broadcasts.size)
                at(t)
                assertEquals("at $t", i + 2, mesh.broadcasts.size)
            }
            assertEquals(1, mesh.broadcasts.distinct().size)
            assertEquals(1, sms.sent.size)
            assertEquals(1, mesh.startCalls)
        }
    }

    @Test
    fun ackStopsEverything() {
        ackStops { mesh.incoming.emit(meshAck(42, 0)) }
        ackStops { sms.incoming.emit(smsAck(42, 0)) }
    }

    private fun ackStops(deliver: suspend EmergencyTestKit.() -> Unit) = runTest {
        with(EmergencyTestKit()) {
            val o = orchestrator()
            o.declare()
            at(6_000)
            assertEquals(2, mesh.broadcasts.size)
            deliver()
            runCurrent()
            assertEquals(EmergencyPhase.NOTIFIED, o.state.value.phase)
            assertEquals(0, o.state.value.ackedSeq)
            advanceTimeBy(600_000)
            assertEquals(2, mesh.broadcasts.size)
            assertEquals(1, sms.sent.size)
        }
    }

    @Test
    fun ackForOtherEmergencyOrBadSignatureIsIgnored() = runTest {
        with(EmergencyTestKit()) {
            val o = orchestrator()
            o.declare()
            runCurrent()
            val forged = MamaText.meshAck(AckCodec.sign(AckInfo(42L, 0), DEVICE))
            val otherFrame = EmergencyFrames.build(Kind.ALERT, DEVICE, 99L, 0, null, NOW).meshText(SERVER_X)
            mesh.incoming.emit(meshAck(43, 0))
            mesh.incoming.emit(forged)
            mesh.incoming.emit(otherFrame)
            mesh.incoming.emit("hola")
            sms.incoming.emit(smsAck(43, 0))
            sms.incoming.emit(MamaText.sms(AckCodec.sign(AckInfo(42L, 0), DEVICE)))
            runCurrent()
            assertEquals(EmergencyPhase.SEARCHING_NEARBY, o.state.value.phase)
            assertNull(o.state.value.ackedSeq)
            at(5_000)
            assertEquals(2, mesh.broadcasts.size)
        }
    }

    @Test
    fun overrideForcesChannels() {
        fun case(override: ChannelOverride, check: EmergencyTestKit.(EmergencyState) -> Unit) = runTest {
            with(EmergencyTestKit()) {
                online = true
                http.behaviour = { httpAck(42, 0) }
                val o = orchestrator(override)
                o.declare()
                at(20_000)
                check(o.state.value)
            }
        }
        case(ChannelOverride.NO_INTERNET) { s ->
            assertTrue(http.posted.isEmpty())
            assertEquals(1, sms.sent.size)
            assertTrue(mesh.broadcasts.isNotEmpty())
            assertEquals(setOf(Channel.SMS, Channel.MESH), s.channelsUsed)
        }
        case(ChannelOverride.SMS_ONLY) { s ->
            assertTrue(http.posted.isEmpty())
            assertEquals(1, sms.sent.size)
            assertEquals(0, mesh.startCalls)
            assertTrue(mesh.broadcasts.isEmpty())
            assertEquals(setOf(Channel.SMS), s.channelsUsed)
            assertNull(s.meshIssue)
        }
        case(ChannelOverride.BLE_ONLY) { s ->
            assertTrue(sms.canSend)
            assertTrue(http.posted.isEmpty())
            assertTrue(sms.sent.isEmpty())
            assertTrue(mesh.broadcasts.isNotEmpty())
            assertEquals(setOf(Channel.MESH), s.channelsUsed)
        }
        case(ChannelOverride.AUTO) { s ->
            assertEquals(1, http.posted.size)
            assertTrue(sms.sent.isEmpty())
            assertTrue(mesh.broadcasts.isEmpty())
            assertEquals(setOf(Channel.HTTP), s.channelsUsed)
            assertEquals(EmergencyPhase.NOTIFIED, s.phase)
        }
    }

    @Test
    fun noServerKeysDisablesMeshButHttpWorks() {
        runTest {
            with(EmergencyTestKit()) {
                config = EmergencyConfig(SMS_NUMBER, null, null)
                val o = orchestrator()
                o.declare()
                at(20_000)
                assertEquals(0, mesh.startCalls)
                assertTrue(mesh.broadcasts.isEmpty())
                assertEquals(MeshIssue.NO_SERVER_KEYS, o.state.value.meshIssue)
                assertEquals(1, sms.sent.size)
                assertEquals(EmergencyPhase.SEARCHING_NEARBY, o.state.value.phase)
                // Without the server Ed25519 key, mesh and SMS acks cannot be trusted.
                mesh.incoming.emit(meshAck(42, 0))
                sms.incoming.emit(smsAck(42, 0))
                runCurrent()
                assertEquals(EmergencyPhase.SEARCHING_NEARBY, o.state.value.phase)
            }
        }
        runTest {
            with(EmergencyTestKit()) {
                config = EmergencyConfig(SMS_NUMBER, null, null)
                online = true
                http.behaviour = { httpAck(42, 0) }
                val o = orchestrator()
                o.declare()
                runCurrent()
                assertEquals(EmergencyPhase.NOTIFIED, o.state.value.phase)
                assertEquals(0, o.state.value.ackedSeq)
            }
        }
    }

    @Test
    fun meshStartRetriedOnTicks() = runTest {
        with(EmergencyTestKit()) {
            sms.canSend = false
            mesh.startResult = false
            val o = orchestrator()
            o.declare()
            runCurrent()
            assertEquals(EmergencyPhase.NO_CHANNEL, o.state.value.phase)
            assertEquals(MeshIssue.UNAVAILABLE, o.state.value.meshIssue)
            assertTrue(mesh.broadcasts.isEmpty())
            assertEquals(1, mesh.startCalls)
            at(4_000)
            mesh.startResult = true
            at(5_000)
            assertEquals(2, mesh.startCalls)
            assertEquals(1, mesh.broadcasts.size)
            assertEquals(EmergencyPhase.SEARCHING_NEARBY, o.state.value.phase)
            assertNull(o.state.value.meshIssue)
            at(15_000)
            assertEquals(2, mesh.startCalls)
            assertEquals(2, mesh.broadcasts.size)
        }
    }

    @Test
    fun httpRetriedOnTickWhenNetworkReturns() = runTest {
        with(EmergencyTestKit()) {
            val o = orchestrator()
            o.declare()
            runCurrent()
            assertTrue(http.posted.isEmpty())
            at(4_000)
            online = true
            http.behaviour = { httpAck(42, 0) }
            at(5_000)
            assertEquals(1, http.posted.size)
            assertEquals(EmergencyPhase.NOTIFIED, o.state.value.phase)
            val count = mesh.broadcasts.size
            advanceTimeBy(600_000)
            assertEquals(count, mesh.broadcasts.size)
            assertEquals(1, http.posted.size)
        }
    }

    @Test
    fun smsSentLaterWhenItBecomesAvailable() = runTest {
        with(EmergencyTestKit()) {
            sms.canSend = false
            val o = orchestrator()
            o.declare()
            runCurrent()
            assertTrue(sms.sent.isEmpty())
            at(4_000)
            sms.canSend = true
            at(5_000)
            assertEquals(1, sms.sent.size)
            advanceTimeBy(600_000)
            assertEquals(1, sms.sent.size)
        }
    }

    @Test
    fun httpFailureIsRetriedOnLaterTicksButNotImmediately() = runTest {
        with(EmergencyTestKit()) {
            online = true
            val o = orchestrator()
            o.declare()
            runCurrent()
            assertEquals(1, http.posted.size)
            at(4_999)
            assertEquals(1, http.posted.size)
            at(5_000)
            assertEquals(2, http.posted.size)
        }
    }

    @Test
    fun http4xxIsNeverRetried() = runTest {
        with(EmergencyTestKit()) {
            online = true
            http.behaviour = { throw ServerHttpException(400, "bad payload") }
            val o = orchestrator()
            o.declare()
            advanceTimeBy(600_000)
            assertEquals(1, http.posted.size)
            assertEquals(1, sms.sent.size)
            assertEquals(EmergencyPhase.SEARCHING_NEARBY, o.state.value.phase)
        }
    }

    @Test
    fun invalidHttpReplyIsAFailure() = runTest {
        with(EmergencyTestKit()) {
            online = true
            http.behaviour = { httpAck(43, 0) }
            val o = orchestrator()
            o.declare()
            runCurrent()
            assertEquals(1, mesh.broadcasts.size)
            assertEquals(EmergencyPhase.SEARCHING_NEARBY, o.state.value.phase)
            assertNull(o.state.value.ackedSeq)
        }
    }

    @Test
    fun noChannelWhenSmsFailsAndMeshIsOff() = runTest {
        with(EmergencyTestKit()) {
            sms.behaviour = { _, _ -> false }
            mesh.startResult = false
            val o = orchestrator()
            o.declare()
            runCurrent()
            assertEquals(1, sms.sent.size)
            assertEquals(EmergencyPhase.NO_CHANNEL, o.state.value.phase)
            at(5_000)
            assertEquals(1, sms.sent.size)
        }
    }

    @Test
    fun declareIsRejectedWhileActive() = runTest {
        with(EmergencyTestKit()) {
            val o = orchestrator()
            assertEquals(EmergencyPhase.IDLE, o.state.value.phase)
            assertTrue(o.declare())
            assertEquals(EmergencyPhase.SENDING, o.state.value.phase)
            assertFalse(o.declare())
            runCurrent()
            assertFalse(o.declare())
            assertEquals(1, sms.sent.size)
        }
    }

    @Test
    fun signingFailureIsNoChannelAndRetried() = runTest {
        with(EmergencyTestKit()) {
            var broken = true
            val flaky = object : PayloadSigner {
                override val publicKey: ByteArray
                    get() = if (broken) throw MeshUnavailableException(IllegalStateException("keystore")) else DEVICE.publicKey

                override fun sign(message: ByteArray) = DEVICE.sign(message)
            }
            val o = EmergencyOrchestrator(
                signer = flaky,
                config = { config },
                http = http,
                sms = sms,
                mesh = mesh,
                location = location,
                network = { online },
                channelOverride = { ChannelOverride.AUTO },
                scope = backgroundScope,
                nowMs = { NOW + testScheduler.currentTime },
                newEmergencyId = { 42L },
            )
            assertTrue(o.declare())
            runCurrent()
            assertEquals(EmergencyPhase.NO_CHANNEL, o.state.value.phase)
            assertEquals(MeshIssue.UNAVAILABLE, o.state.value.meshIssue)
            assertTrue(sms.sent.isEmpty())
            assertTrue(mesh.broadcasts.isEmpty())
            broken = false
            at(5_000)
            assertEquals(1, sms.sent.size)
            assertEquals(1, mesh.broadcasts.size)
            assertEquals(EmergencyPhase.SEARCHING_NEARBY, o.state.value.phase)
            assertNull(o.state.value.meshIssue)
        }
    }

    @Test
    fun cancelStopsEmission() = runTest {
        with(EmergencyTestKit()) {
            val o = orchestrator()
            o.declare()
            runCurrent()
            o.cancel()
            runCurrent()
            assertEquals(EmergencyPhase.CANCELLED, o.state.value.phase)
            advanceTimeBy(600_000)
            assertEquals(1, mesh.broadcasts.size)
            assertTrue(o.declare())
        }
    }
}
