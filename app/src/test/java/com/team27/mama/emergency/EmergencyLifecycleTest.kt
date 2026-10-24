package com.team27.mama.emergency

import com.team27.mama.emergency.EmergencyTestKit.Companion.NOW
import com.team27.mama.emergency.EmergencyTestKit.Companion.decodeMesh
import com.team27.mama.emergency.EmergencyTestKit.Companion.decodeSms
import com.team27.mama.emergency.EmergencyTestKit.Companion.httpAck
import com.team27.mama.emergency.EmergencyTestKit.Companion.meshAck
import com.team27.mama.emergency.payload.Kind
import com.team27.mama.emergency.payload.PayloadCodec
import com.team27.mama.emergency.payload.PayloadFormat
import java.io.IOException
import kotlinx.coroutines.ExperimentalCoroutinesApi
import kotlinx.coroutines.awaitCancellation
import kotlinx.coroutines.test.TestScope
import kotlinx.coroutines.test.advanceTimeBy
import kotlinx.coroutines.test.runCurrent
import kotlinx.coroutines.test.runTest
import org.junit.Assert.assertEquals
import org.junit.Assert.assertFalse
import org.junit.Assert.assertTrue
import org.junit.Test

@OptIn(ExperimentalCoroutinesApi::class)
class EmergencyLifecycleTest {

    /** Advances virtual time to exactly [t] ms after the test started and runs what is due. */
    private fun TestScope.at(t: Long) {
        advanceTimeBy(t - testScheduler.currentTime)
        runCurrent()
    }

    private val freshFix = LocationFix(-33.4, -70.6, 0)

    /** HTTP acks every post with the seq it carries. */
    private fun EmergencyTestKit.httpAcksEverything() {
        online = true
        http.behaviour = { text -> httpAck(42, decodeSms(text).seq) }
    }

    @Test
    fun freshFixReemitsWithSeqPlusOne() = runTest {
        with(EmergencyTestKit()) {
            location.lastKnown = LocationFix(-33.0, -70.0, NOW - 600_000)
            val o = orchestrator()
            o.declare()
            runCurrent()
            assertEquals(1, location.freshCalls)
            assertEquals(600, decodeMesh(mesh.broadcasts.single()).locAgeS)
            at(20_000)
            val before = mesh.broadcasts.size
            location.fresh.complete(freshFix.copy(timeMs = NOW + 20_000))
            runCurrent()
            assertEquals(before + 1, mesh.broadcasts.size)
            val p = decodeMesh(mesh.broadcasts.last())
            assertEquals(Kind.UPDATE, p.kind)
            assertEquals(1, p.seq)
            assertEquals(42L, p.emergencyId)
            assertEquals(0, p.locAgeS)
            assertEquals(PayloadCodec.degreesToE5(-33.4), p.latE5)
            assertEquals(PayloadCodec.degreesToE5(-70.6), p.lonE5)
            assertEquals(2, sms.sent.size)
            assertEquals(p, decodeSms(sms.sent[1].second))
            at(600_000)
            val after = mesh.broadcasts.drop(before)
            assertTrue(after.size > 1)
            assertEquals(1, after.distinct().size)
            assertEquals(2, sms.sent.size)
            assertEquals(1, o.state.value.seq)
        }
    }

    @Test
    fun recentLastKnownWaitsForNoFix() = runTest {
        with(EmergencyTestKit()) {
            location.lastKnown = LocationFix(-33.0, -70.0, NOW - 10_000)
            val o = orchestrator()
            o.declare()
            at(600_000)
            assertEquals(0, location.freshCalls)
            assertTrue(mesh.broadcasts.size > 1)
            assertTrue(mesh.broadcasts.all { decodeMesh(it).seq == 0 })
            assertTrue(sms.sent.all { decodeSms(it.second).seq == 0 })
            assertEquals(10, decodeMesh(mesh.broadcasts.first()).locAgeS)
            assertEquals(0, o.state.value.seq)
        }
    }

    @Test
    fun noFixSendsNoLocationAndGivesUpAfterTimeout() = runTest {
        with(EmergencyTestKit()) {
            val o = orchestrator()
            o.declare()
            runCurrent()
            val first = decodeMesh(mesh.broadcasts.single())
            assertEquals(PayloadFormat.NO_LOCATION, first.locAgeS)
            assertEquals(0, first.latE5)
            assertEquals(0, first.lonE5)
            assertEquals(1, location.freshCalls)
            at(119_999)
            assertFalse(location.freshCancelled)
            at(120_000)
            assertTrue(location.freshCancelled)
            location.fresh.complete(freshFix.copy(timeMs = NOW + 120_000))
            at(600_000)
            assertTrue(mesh.broadcasts.all { decodeMesh(it).seq == 0 })
            assertEquals(1, sms.sent.size)
            assertEquals(0, o.state.value.seq)
        }
    }

    @Test
    fun freshFixNotNewerThanTheOneUsedIsIgnored() = runTest {
        with(EmergencyTestKit()) {
            location.lastKnown = LocationFix(-33.0, -70.0, NOW - 600_000)
            val o = orchestrator()
            o.declare()
            at(1_000)
            location.fresh.complete(freshFix.copy(timeMs = NOW - 600_000))
            at(600_000)
            assertTrue(mesh.broadcasts.all { decodeMesh(it).seq == 0 })
            assertEquals(1, sms.sent.size)
            assertEquals(0, o.state.value.seq)
        }
    }

    @Test
    fun updateThatCannotBeSignedKeepsTheAlertGoing() = runTest {
        with(EmergencyTestKit()) {
            val signer = FlakySigner(broken = false)
            val o = orchestrator(signer = signer)
            o.declare()
            at(1_000)
            signer.broken = true
            location.fresh.complete(freshFix.copy(timeMs = NOW + 1_000))
            runCurrent()
            at(5_000)
            assertEquals(2, mesh.broadcasts.size)
            assertTrue(mesh.broadcasts.all { decodeMesh(it).kind == Kind.ALERT })
            assertEquals(EmergencyPhase.SEARCHING_NEARBY, o.state.value.phase)
            signer.broken = false
            at(6_000)
            val p = decodeMesh(mesh.broadcasts.last())
            assertEquals(Kind.UPDATE to 1, p.kind to p.seq)
        }
    }

    @Test
    fun updateAfterHttpAckGoesOverHttp() = runTest {
        with(EmergencyTestKit()) {
            httpAcksEverything()
            val o = orchestrator()
            o.declare()
            runCurrent()
            assertEquals(EmergencyPhase.NOTIFIED, o.state.value.phase)
            at(1_000)
            location.fresh.complete(freshFix.copy(timeMs = NOW + 1_000))
            runCurrent()
            assertEquals(listOf(Kind.ALERT to 0, Kind.UPDATE to 1), http.posted.map { decodeSms(it).let { p -> p.kind to p.seq } })
            assertEquals(EmergencyPhase.NOTIFIED, o.state.value.phase)
            assertEquals(1, o.state.value.ackedSeq)
            assertEquals(1, o.state.value.seq)
            advanceTimeBy(600_000)
            assertEquals(2, http.posted.size)
            assertTrue(sms.sent.isEmpty())
            assertEquals(0, mesh.startCalls)
            assertTrue(mesh.broadcasts.isEmpty())
            assertEquals(setOf(Channel.HTTP), o.state.value.channelsUsed)
        }
    }

    @Test
    fun updateAfterAckStaysNotifiedDuringFallback() = runTest {
        with(EmergencyTestKit()) {
            online = true
            http.behaviour = { text -> if (decodeSms(text).seq == 0) httpAck(42, 0) else throw IOException("boom") }
            val o = orchestrator()
            o.declare()
            runCurrent()
            location.fresh.complete(freshFix.copy(timeMs = NOW))
            runCurrent()
            assertEquals(2, http.posted.size)
            assertEquals(Kind.UPDATE, decodeMesh(mesh.broadcasts.single()).kind)
            assertEquals(EmergencyPhase.NOTIFIED, o.state.value.phase)
            at(5_000)
            assertEquals(2, mesh.broadcasts.size)
            assertEquals(EmergencyPhase.NOTIFIED, o.state.value.phase)
        }
    }

    @Test
    fun staleAckNotifiesButKeepsEmittingUpdate() = runTest {
        with(EmergencyTestKit()) {
            val o = orchestrator()
            o.declare()
            at(1_000)
            location.fresh.complete(freshFix.copy(timeMs = NOW + 1_000))
            runCurrent()
            assertEquals(1, decodeMesh(mesh.broadcasts.last()).seq)
            at(2_000)
            mesh.incoming.emit(meshAck(42, 0))
            runCurrent()
            assertEquals(EmergencyPhase.NOTIFIED, o.state.value.phase)
            assertEquals(0, o.state.value.ackedSeq)
            val n = mesh.broadcasts.size
            at(6_000)
            assertEquals(n + 1, mesh.broadcasts.size)
            assertEquals(1, decodeMesh(mesh.broadcasts.last()).seq)
            mesh.incoming.emit(meshAck(42, 1))
            runCurrent()
            assertEquals(1, o.state.value.ackedSeq)
            val m = mesh.broadcasts.size
            advanceTimeBy(600_000)
            assertEquals(m, mesh.broadcasts.size)
        }
    }

    @Test
    fun cancelEmitsSignedCancelOverSameChannels() = runTest {
        with(EmergencyTestKit()) {
            location.lastKnown = LocationFix(-33.4, -70.6, NOW - 10_000)
            val o = orchestrator()
            o.declare()
            at(6_000)
            val before = mesh.broadcasts.size
            o.cancel()
            assertEquals(EmergencyPhase.CANCELLING, o.state.value.phase)
            runCurrent()
            val p = decodeMesh(mesh.broadcasts.drop(before).single())
            assertEquals(Kind.CANCEL, p.kind)
            assertEquals(1, p.seq)
            assertEquals(42L, p.emergencyId)
            assertEquals(16, p.locAgeS)
            assertEquals(PayloadCodec.degreesToE5(-33.4), p.latE5)
            assertEquals(2, sms.sent.size)
            assertEquals(p, decodeSms(sms.sent[1].second))
            assertEquals(1, o.state.value.seq)
            assertEquals(EmergencyPhase.CANCELLING, o.state.value.phase)
            at(30_000)
            val after = mesh.broadcasts.drop(before)
            assertEquals(3, after.size)
            assertTrue(after.all { decodeMesh(it).kind == Kind.CANCEL && decodeMesh(it).seq == 1 })
            mesh.incoming.emit(meshAck(42, 1))
            runCurrent()
            assertEquals(EmergencyPhase.CANCELLED, o.state.value.phase)
            val n = mesh.broadcasts.size
            advanceTimeBy(600_000)
            assertEquals(n, mesh.broadcasts.size)
            assertEquals(2, sms.sent.size)
            assertTrue(o.declare())
        }
    }

    @Test
    fun cancelCarriesTheNewestFix() = runTest {
        with(EmergencyTestKit()) {
            val o = orchestrator()
            o.declare()
            at(130_000)
            location.lastKnown = freshFix.copy(timeMs = NOW + 125_000)
            o.cancel()
            runCurrent()
            val p = decodeMesh(mesh.broadcasts.last())
            assertEquals(Kind.CANCEL, p.kind)
            assertEquals(5, p.locAgeS)
            assertEquals(PayloadCodec.degreesToE5(-33.4), p.latE5)
        }
    }

    @Test
    fun staleAckDuringCancelKeepsCancelling() = runTest {
        with(EmergencyTestKit()) {
            val o = orchestrator()
            o.declare()
            at(6_000)
            o.cancel()
            runCurrent()
            mesh.incoming.emit(meshAck(42, 0))
            runCurrent()
            assertEquals(EmergencyPhase.CANCELLING, o.state.value.phase)
            assertEquals(0, o.state.value.ackedSeq)
            val n = mesh.broadcasts.size
            at(11_000)
            assertEquals(n + 1, mesh.broadcasts.size)
            assertEquals(Kind.CANCEL, decodeMesh(mesh.broadcasts.last()).kind)
        }
    }

    @Test
    fun cancelThatCannotBeSignedStillStopsTheAlert() = runTest {
        with(EmergencyTestKit()) {
            val signer = FlakySigner(broken = false)
            val o = orchestrator(signer = signer)
            o.declare()
            at(6_000)
            val before = mesh.broadcasts.size
            signer.broken = true
            o.cancel()
            // The alert would tick at 15 s; the cancel build is retried at 11 s and 21 s.
            at(15_000)
            assertEquals(before, mesh.broadcasts.size)
            assertEquals(EmergencyPhase.CANCELLING, o.state.value.phase)
            assertEquals(MeshIssue.UNAVAILABLE, o.state.value.meshIssue)
            signer.broken = false
            at(21_000)
            val p = decodeMesh(mesh.broadcasts.drop(before).single())
            assertEquals(Kind.CANCEL to 1, p.kind to p.seq)
            assertEquals(EmergencyPhase.CANCELLING, o.state.value.phase)
        }
    }

    @Test
    fun cancelAfterHttpAckUsesHttp() = runTest {
        with(EmergencyTestKit()) {
            httpAcksEverything()
            val o = orchestrator()
            o.declare()
            runCurrent()
            assertEquals(EmergencyPhase.NOTIFIED, o.state.value.phase)
            o.cancel()
            assertEquals(EmergencyPhase.CANCELLING, o.state.value.phase)
            runCurrent()
            assertTrue(location.freshCancelled)
            assertEquals(2, http.posted.size)
            val p = decodeSms(http.posted[1])
            assertEquals(Kind.CANCEL, p.kind)
            assertEquals(1, p.seq)
            assertEquals(EmergencyPhase.CANCELLED, o.state.value.phase)
            assertEquals(1, o.state.value.ackedSeq)
            location.fresh.complete(freshFix.copy(timeMs = NOW))
            advanceTimeBy(600_000)
            assertEquals(2, http.posted.size)
            assertTrue(sms.sent.isEmpty())
            assertEquals(0, mesh.startCalls)
            assertTrue(mesh.broadcasts.isEmpty())
            assertEquals(setOf(Channel.HTTP), o.state.value.channelsUsed)
        }
    }

    @Test
    fun cancelDuringHttpAttempt() = runTest {
        with(EmergencyTestKit()) {
            online = true
            http.behaviour = { awaitCancellation() }
            val o = orchestrator()
            o.declare()
            at(2_000)
            assertEquals(EmergencyPhase.SENDING, o.state.value.phase)
            o.cancel()
            runCurrent()
            assertEquals(2, http.posted.size)
            val p = decodeSms(http.posted[1])
            assertEquals(Kind.CANCEL, p.kind)
            assertEquals(1, p.seq)
            at(6_999)
            assertTrue(sms.sent.isEmpty())
            assertTrue(mesh.broadcasts.isEmpty())
            assertEquals(EmergencyPhase.CANCELLING, o.state.value.phase)
            at(7_000)
            assertEquals(p, decodeSms(sms.sent.single().second))
            assertEquals(p, decodeMesh(mesh.broadcasts.single()))
            at(600_000)
            assertTrue(mesh.broadcasts.all { decodeMesh(it).kind == Kind.CANCEL })
            assertEquals(1, sms.sent.size)
        }
    }

    @Test
    fun cancelWindowEnds() = runTest {
        with(EmergencyTestKit()) {
            val o = orchestrator()
            o.declare()
            at(6_000)
            val before = mesh.broadcasts.size
            o.cancel()
            runCurrent()
            assertTrue(location.freshCancelled)
            location.fresh.complete(freshFix.copy(timeMs = NOW + 6_000))
            at(6_000 + 119_999)
            assertEquals(EmergencyPhase.CANCELLING, o.state.value.phase)
            assertTrue(mesh.broadcasts.drop(before).all { decodeMesh(it).kind == Kind.CANCEL })
            at(6_000 + 120_000)
            assertEquals(EmergencyPhase.CANCELLED, o.state.value.phase)
            val n = mesh.broadcasts.size
            advanceTimeBy(600_000)
            assertEquals(n, mesh.broadcasts.size)
            assertEquals(2, sms.sent.size)
        }
    }

    @Test
    fun declareWhileActiveReturnsFalse() {
        runTest {
            with(EmergencyTestKit()) {
                val o = orchestrator()
                o.declare()
                runCurrent()
                assertEquals(EmergencyPhase.SEARCHING_NEARBY, o.state.value.phase)
                assertFalse(o.declare())
                runCurrent()
                assertEquals(1, mesh.broadcasts.size)
                assertEquals(1, sms.sent.size)
                o.cancel()
                assertEquals(EmergencyPhase.CANCELLING, o.state.value.phase)
                assertFalse(o.declare())
                runCurrent()
                assertEquals(listOf(Kind.ALERT, Kind.CANCEL), mesh.broadcasts.map { decodeMesh(it).kind })
                assertEquals(2, sms.sent.size)
            }
        }
        runTest {
            with(EmergencyTestKit()) {
                httpAcksEverything()
                val o = orchestrator()
                o.declare()
                runCurrent()
                assertEquals(EmergencyPhase.NOTIFIED, o.state.value.phase)
                assertFalse(o.declare())
                runCurrent()
                assertEquals(1, http.posted.size)
            }
        }
        runTest {
            with(EmergencyTestKit()) {
                val o = orchestrator()
                o.cancel()
                runCurrent()
                assertEquals(EmergencyState(), o.state.value)
                advanceTimeBy(600_000)
                assertTrue(mesh.broadcasts.isEmpty())
                assertTrue(sms.sent.isEmpty())
                assertTrue(o.declare())
            }
        }
    }

    @Test
    fun cancelBeforeTheScopeRunsSendsNothing() = runTest {
        with(EmergencyTestKit()) {
            val o = orchestrator()
            o.declare()
            o.cancel()
            o.cancel()
            assertFalse(o.declare())
            runCurrent()
            assertEquals(EmergencyPhase.CANCELLED, o.state.value.phase)
            advanceTimeBy(600_000)
            assertTrue(http.posted.isEmpty())
            assertTrue(sms.sent.isEmpty())
            assertTrue(mesh.broadcasts.isEmpty())
            assertEquals(0, location.freshCalls)
            assertTrue(o.declare())
            runCurrent()
            assertEquals(EmergencyPhase.SEARCHING_NEARBY, o.state.value.phase)
            assertEquals(Kind.ALERT, decodeMesh(mesh.broadcasts.single()).kind)
        }
    }

    @Test
    fun cancelBeforeTheAlertCouldBeSignedSendsNothing() = runTest {
        with(EmergencyTestKit()) {
            val signer = FlakySigner()
            val o = orchestrator(signer = signer)
            o.declare()
            runCurrent()
            assertEquals(EmergencyPhase.NO_CHANNEL, o.state.value.phase)
            o.cancel()
            runCurrent()
            assertEquals(EmergencyPhase.CANCELLED, o.state.value.phase)
            signer.broken = false
            advanceTimeBy(600_000)
            assertTrue(sms.sent.isEmpty())
            assertTrue(mesh.broadcasts.isEmpty())
            assertTrue(o.declare())
        }
    }

    @Test
    fun lateCancelNeverTouchesTheNextEmergency() = runTest {
        with(EmergencyTestKit()) {
            val o = orchestrator()
            o.declare()
            runCurrent()
            o.cancel()
            o.cancel()
            runCurrent()
            mesh.incoming.emit(meshAck(42, 1))
            runCurrent()
            assertEquals(EmergencyPhase.CANCELLED, o.state.value.phase)
            // A cancel that lands after the end, then a new emergency before the scope drains.
            o.cancel()
            assertTrue(o.declare())
            runCurrent()
            assertEquals(EmergencyPhase.SEARCHING_NEARBY, o.state.value.phase)
            assertEquals(0, o.state.value.seq)
            val alert = decodeMesh(mesh.broadcasts.last())
            assertEquals(Kind.ALERT to 0, alert.kind to alert.seq)
            val n = mesh.broadcasts.size
            at(testScheduler.currentTime + 5_000)
            assertEquals(n + 1, mesh.broadcasts.size)
            assertEquals(EmergencyPhase.SEARCHING_NEARBY, o.state.value.phase)
        }
    }
}
