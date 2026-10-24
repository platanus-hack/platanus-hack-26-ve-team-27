package com.team27.mama.relay

import com.team27.mama.emergency.payload.MamaText
import com.team27.mama.onboarding.NetworkStatus
import kotlin.coroutines.cancellation.CancellationException
import kotlinx.coroutines.CompletableDeferred
import kotlinx.coroutines.CoroutineScope
import kotlinx.coroutines.ExperimentalCoroutinesApi
import kotlinx.coroutines.Job
import kotlinx.coroutines.awaitCancellation
import kotlinx.coroutines.cancel
import kotlinx.coroutines.delay
import kotlinx.coroutines.test.TestScope
import kotlinx.coroutines.test.advanceTimeBy
import kotlinx.coroutines.test.runCurrent
import kotlinx.coroutines.test.runTest
import org.junit.Assert.assertEquals
import org.junit.Assert.assertTrue
import org.junit.Test

@OptIn(ExperimentalCoroutinesApi::class)
class MeshRelayTest {

    @Test
    fun onlineUploadsVerbatimAndRebroadcastsAck() = runTest {
        with(RelayTestKit()) {
            network.online = true
            val r = relay()
            hear(heard(seal(1), hops = 2))
            assertEquals(listOf(seal(1) to 2), uploader.calls)
            assertEquals(listOf(ACK), mesh.sent)
            assertEquals(1, r.relayedCount.value)
            assertEquals(0, r.pendingCount.value)
            val s = saved()!!
            assertEquals(1, s.relayedCount)
            assertTrue(s.pending.isEmpty())
            assertEquals(ACK, s.seen.single().ackText)
            assertEquals(relayHash(seal(1)), s.seen.single().hash)
        }
    }

    @Test
    fun offlineQueuesThenUploadsOnReconnect() = runTest {
        with(RelayTestKit()) {
            val r = relay()
            hear(heard(seal(1), hops = 1))
            assertTrue(uploader.calls.isEmpty())
            assertEquals(1, r.pendingCount.value)
            assertTrue(store.json!!.contains(seal(1)))
            val p = saved()!!.pending.single()
            assertEquals(seal(1), p.text)
            assertEquals(1, p.hops)
            assertEquals(T0, p.heardAtMs)

            at(1_000)
            network.online = true
            r.onNetworkAvailable()
            runCurrent()
            assertEquals(listOf(seal(1) to 1), uploader.calls)
            assertEquals(listOf(ACK), mesh.sent)
            assertEquals(0, r.pendingCount.value)
            assertEquals(1, r.relayedCount.value)
        }
    }

    @Test
    fun offlinePollFindsNetworkWithoutCallback() = runTest {
        with(RelayTestKit()) {
            relay()
            hear(heard(seal(1)))
            network.online = true
            at(59_999)
            assertTrue(uploader.calls.isEmpty())
            at(60_000)
            assertEquals(1, uploader.calls.size)
        }
    }

    @Test
    fun offlineWithNothingPendingNeverPolls() = runTest {
        with(RelayTestKit()) {
            val polls = mutableListOf<Long>()
            val counting = object : NetworkStatus {
                override fun isOnline(): Boolean {
                    polls += testScheduler.currentTime
                    return false
                }
            }
            MeshRelay(mesh, uploader, counting, store, backgroundScope, { T0 + testScheduler.currentTime }).start()
            runCurrent()
            val before = polls.size
            advanceTimeBy(3_600_000)
            runCurrent()
            assertEquals(before, polls.size)
        }
    }

    @Test
    fun duplicateUploadedOnce() = runTest {
        with(RelayTestKit()) {
            val r = relay()
            hear(heard(seal(1), hops = 0))
            hear(heard(seal(1), hops = 1))
            hear(heard(seal(1), hops = 3))
            assertEquals(1, r.pendingCount.value)
            network.online = true
            r.onNetworkAvailable()
            runCurrent()
            hear(heard(seal(1), hops = 2))
            assertEquals(listOf(seal(1) to 0), uploader.calls)
            assertEquals(1, r.relayedCount.value)
        }
    }

    @Test
    fun duplicateWhileUploadInFlightUploadedOnce() = runTest {
        with(RelayTestKit()) {
            network.online = true
            val gate = CompletableDeferred<UploadResult>()
            uploader.behaviour = { gate.await() }
            val r = relay()
            hear(heard(seal(1)))
            assertEquals(1, uploader.calls.size)
            hear(heard(seal(1), hops = 1))
            assertEquals(1, r.pendingCount.value)
            gate.complete(UploadResult.Acked(ACK))
            runCurrent()
            advanceTimeBy(3_600_000)
            runCurrent()
            assertEquals(1, uploader.calls.size)
            assertEquals(0, r.pendingCount.value)
            assertEquals(1, r.relayedCount.value)
            assertEquals(listOf(ACK), mesh.sent)
        }
    }

    @Test
    fun repeatAfterAckRebroadcastsStoredAck() = runTest {
        with(RelayTestKit()) {
            network.online = true
            relay()
            hear(heard(seal(1)))
            assertEquals(listOf(ACK), mesh.sent)
            at(5_000)
            hear(heard(seal(1)))
            assertEquals(listOf(ACK), mesh.sent)
            at(9_999)
            hear(heard(seal(1)))
            assertEquals(listOf(ACK), mesh.sent)
            at(10_000)
            hear(heard(seal(1)))
            assertEquals(listOf(ACK, ACK), mesh.sent)
            at(12_000)
            hear(heard(seal(1)))
            assertEquals(listOf(ACK, ACK), mesh.sent)
            assertEquals(1, uploader.calls.size)
            assertEquals(T0 + 10_000, saved()!!.seen.single().lastAckBroadcastAtMs)
        }
    }

    @Test
    fun ackNotSentWhenMeshDown() = runTest {
        with(RelayTestKit()) {
            network.online = true
            mesh.broadcastResult = false
            relay()
            hear(heard(seal(1)))
            assertEquals(listOf(ACK), mesh.sent) // attempted and refused
            assertEquals(0L, saved()!!.seen.single().lastAckBroadcastAtMs)
            mesh.broadcastResult = true
            at(1_000)
            hear(heard(seal(1)))
            assertEquals(listOf(ACK, ACK), mesh.sent)
            at(2_000)
            hear(heard(seal(1)))
            assertEquals(listOf(ACK, ACK), mesh.sent)
            assertEquals(1, uploader.calls.size)
        }
    }

    @Test
    fun rejectedIsDroppedForGood() = runTest {
        with(RelayTestKit()) {
            network.online = true
            uploader.behaviour = { UploadResult.Rejected }
            val r = relay()
            hear(heard(seal(1)))
            assertEquals(1, uploader.calls.size)
            assertEquals(0, r.pendingCount.value)
            assertTrue(mesh.sent.isEmpty())
            assertEquals(0, r.relayedCount.value)
            advanceTimeBy(3_600_000)
            runCurrent()
            assertEquals(1, uploader.calls.size)
            hear(heard(seal(1)))
            assertEquals(1, uploader.calls.size)
            assertTrue(mesh.sent.isEmpty())
            assertEquals(1, saved()!!.seen.size)
        }
    }

    @Test
    fun failedUploadRetriesWithCappedBackoff() = runTest {
        with(RelayTestKit()) {
            network.online = true
            uploader.behaviour = { if (uploader.calls.size <= 8) UploadResult.Failed else UploadResult.Acked(ACK) }
            val r = relay()
            assertBackoff(this, r)
        }
    }

    @Test
    fun throwingUploaderRetriesLikeFailed() = runTest {
        with(RelayTestKit()) {
            network.online = true
            uploader.behaviour = { if (uploader.calls.size <= 8) throw IllegalStateException("boom") else UploadResult.Acked(ACK) }
            val r = relay()
            assertBackoff(this, r)
        }
    }

    /** Tries land at t = 0, 5, 15, 35, 75, 155, 315, 615, 915 s; the 9th is Acked. */
    private suspend fun TestScope.assertBackoff(kit: RelayTestKit, r: MeshRelay) = with(kit) {
        hear(heard(seal(1)))
        val times = listOf(0L, 5_000, 15_000, 35_000, 75_000, 155_000, 315_000, 615_000, 915_000)
        for ((i, t) in times.withIndex()) {
            if (t > 0) {
                at(t - 1)
                assertEquals("calls just before t=$t", i, uploader.calls.size)
                assertEquals(0, r.relayedCount.value)
                assertEquals(1, r.pendingCount.value)
            }
            at(t)
            assertEquals("calls at t=$t", i + 1, uploader.calls.size)
        }
        assertEquals(1, r.relayedCount.value)
        assertEquals(0, r.pendingCount.value)
        assertEquals(listOf(ACK), mesh.sent)
        advanceTimeBy(3_600_000)
        runCurrent()
        assertEquals(9, uploader.calls.size)
    }

    @Test
    fun ackedWithInvalidAckTextIsFailed() = runTest {
        with(RelayTestKit()) {
            network.online = true
            uploader.behaviour = {
                if (uploader.calls.size == 1) UploadResult.Acked("<html>portal</html>") else UploadResult.Acked(ACK)
            }
            val r = relay()
            hear(heard(seal(1)))
            assertEquals(1, uploader.calls.size)
            assertEquals(0, r.relayedCount.value)
            assertTrue(mesh.sent.isEmpty())
            at(5_000)
            assertEquals(2, uploader.calls.size)
            assertEquals(1, r.relayedCount.value)
            assertEquals(listOf(ACK), mesh.sent)
        }
    }

    @Test
    fun cancellationIsNotAFailure() = runTest {
        with(RelayTestKit()) {
            network.online = true
            uploader.behaviour = { awaitCancellation() }
            val scope = CoroutineScope(backgroundScope.coroutineContext + Job())
            val r = relay(scope)
            hear(heard(seal(1)))
            assertEquals(1, uploader.calls.size)
            val saves = store.saves
            scope.cancel()
            runCurrent()
            assertEquals(saves, store.saves)
            assertEquals(0, saved()!!.pending.single().attempts)
            assertEquals(1, r.pendingCount.value)
        }
    }

    @Test
    fun uploaderThrowingCancellationStopsWithoutCountingAFailure() = runTest {
        with(RelayTestKit()) {
            network.online = true
            uploader.behaviour = { throw CancellationException("stop") }
            relay()
            hear(heard(seal(1)))
            assertEquals(1, uploader.calls.size)
            assertEquals(0, saved()!!.pending.single().attempts)
        }
    }

    @Test
    fun ownTextsAreNeverUploaded() = runTest {
        with(RelayTestKit()) {
            network.online = true
            val r = relay()
            r.markOwn(seal(1))
            hear(heard(seal(1)))
            assertTrue(uploader.calls.isEmpty())
            hear(heard(seal(2), sender = mesh.ownPeerId))
            assertTrue(uploader.calls.isEmpty())
            hear(heard(seal(3)))
            assertEquals(listOf(seal(3) to 0), uploader.calls)
            assertEquals(1, r.relayedCount.value)
            assertEquals(listOf(relayHash(seal(3))), saved()!!.seen.map { it.hash })
        }
    }

    @Test
    fun ownSetKeepsOnlyTheNewestTexts() = runTest {
        with(RelayTestKit()) {
            network.online = true
            val r = relay()
            for (n in 0 until RelayLimits.MAX_OWN + 1) r.markOwn(seal(n))
            hear(heard(seal(RelayLimits.MAX_OWN)))
            hear(heard(seal(1)))
            assertTrue(uploader.calls.isEmpty())
            hear(heard(seal(0)))
            assertEquals(listOf(seal(0) to 0), uploader.calls)
        }
    }

    @Test
    fun ignoresNonSealAndMalformedTexts() = runTest {
        with(RelayTestKit()) {
            network.online = true
            val r = relay()
            val junk = listOf(
                ACK,
                "MAMA1:hola",
                "MAMA1:SEAL:%%%",
                MamaText.meshSealed(ByteArray(145)),
                MamaText.meshSealed(ByteArray(147)),
                MamaText.meshSealed(ByteArray(400)),
                "hola",
            )
            for (text in junk) hear(heard(text))
            assertTrue(uploader.calls.isEmpty())
            assertEquals(0, r.pendingCount.value)
            assertEquals(0, r.relayedCount.value)
            assertTrue(mesh.sent.isEmpty())
            val s = saved()
            assertTrue(s == null || (s.pending.isEmpty() && s.seen.isEmpty()))
        }
    }

    @Test
    fun counterCountsDistinctDeliveries() = runTest {
        with(RelayTestKit()) {
            network.online = true
            uploader.behaviour = { if (it == seal(3)) UploadResult.Rejected else UploadResult.Acked(ACK) }
            val r = relay()
            hear(heard(seal(1)))
            hear(heard(seal(2)))
            hear(heard(seal(3)))
            hear(heard(seal(1)))
            assertEquals(2, r.relayedCount.value)
            assertEquals(3, uploader.calls.size)
            assertEquals(2, saved()!!.relayedCount)
        }
    }

    @Test
    fun uploadsOldestHeardFirstOneAtATime() = runTest {
        with(RelayTestKit()) {
            var inFlight = 0
            var maxInFlight = 0
            uploader.behaviour = {
                inFlight++
                maxInFlight = maxOf(maxInFlight, inFlight)
                delay(100)
                inFlight--
                UploadResult.Acked(ACK)
            }
            val r = relay()
            hear(heard(seal(1)))
            hear(heard(seal(2)))
            hear(heard(seal(3)))
            network.online = true
            r.onNetworkAvailable()
            advanceTimeBy(1_000)
            runCurrent()
            assertEquals(listOf(seal(1), seal(2), seal(3)), uploader.calls.map { it.first })
            assertEquals(1, maxInFlight)
            assertEquals(3, r.relayedCount.value)
        }
    }

    @Test
    fun pendingQueueDropsOldestWhenFull() = runTest {
        with(RelayTestKit()) {
            val r = relay()
            for (n in 0..RelayLimits.MAX_PENDING) hear(heard(seal(n)))
            assertEquals(RelayLimits.MAX_PENDING, r.pendingCount.value)
            val pending = saved()!!.pending.map { it.text }
            assertEquals(seal(1), pending.first())
            assertEquals(seal(RelayLimits.MAX_PENDING), pending.last())
            hear(heard(seal(0)))
            assertEquals(RelayLimits.MAX_PENDING, r.pendingCount.value)
        }
    }

    @Test
    fun startIsIdempotent() = runTest {
        with(RelayTestKit()) {
            network.online = true
            val r = relay()
            r.start()
            runCurrent()
            hear(heard(seal(1)))
            assertEquals(1, uploader.calls.size)
            assertEquals(listOf(ACK), mesh.sent)
        }
    }
}
