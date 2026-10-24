package com.team27.mama.relay

import com.team27.mama.emergency.payload.MamaText
import kotlinx.coroutines.CoroutineScope
import kotlinx.coroutines.ExperimentalCoroutinesApi
import kotlinx.coroutines.Job
import kotlinx.coroutines.cancel
import kotlinx.coroutines.test.runCurrent
import kotlinx.coroutines.test.runTest
import org.junit.Assert.assertEquals
import org.junit.Assert.assertFalse
import org.junit.Assert.assertTrue
import org.junit.Test

/** Persistence, bounds and the 12 h expiry of [MeshRelay] (Task 3 of #60). */
@OptIn(ExperimentalCoroutinesApi::class)
class MeshRelayStateTest {

    /** A second, distinct ack, so a test can tell which delivery a broadcast belongs to. */
    private val otherAck: String = MamaText.meshAck(ByteArray(RelayLimits.ACK_BYTES) { 5 })

    /** 146-byte boxes that stay distinct beyond 256 (unlike [seal]). */
    private fun distinct(n: Int): String =
        MamaText.meshSealed(ByteArray(146) { i -> if (i < 2) (n shr (8 * i)).toByte() else 0 })

    @Test
    fun restartRestoresQueueSeenAndCounter() = runTest {
        val first = RelayTestKit()
        with(first) {
            network.online = true
            val scope = CoroutineScope(backgroundScope.coroutineContext + Job())
            val r = relay(scope)
            hear(heard(seal(1)))
            assertEquals(1, r.relayedCount.value)
            network.online = false
            hear(heard(seal(2), hops = 3))
            assertEquals(1, r.pendingCount.value)
            // The process dies.
            scope.cancel()
            runCurrent()
        }
        with(RelayTestKit(first.store)) {
            at(20_000)
            network.online = true
            uploader.behaviour = { UploadResult.Acked(otherAck) }
            val r = relay()
            assertEquals(listOf(seal(2) to 3), uploader.calls)
            assertEquals(2, r.relayedCount.value)
            assertEquals(0, r.pendingCount.value)
            assertEquals(listOf(otherAck), mesh.sent)
            hear(heard(seal(1)))
            assertEquals(1, uploader.calls.size)
            assertEquals(listOf(otherAck, ACK), mesh.sent)
        }
    }

    @Test
    fun pendingExpiresAfter12Hours() = runTest {
        with(RelayTestKit()) {
            val r = relay()
            hear(heard(seal(1)))
            at(43_199_999)
            assertEquals(1, r.pendingCount.value)
            at(43_200_000)
            assertEquals(0, r.pendingCount.value)
            assertTrue(saved()!!.pending.isEmpty())
            network.online = true
            r.onNetworkAvailable()
            runCurrent()
            assertTrue(uploader.calls.isEmpty())
        }
    }

    @Test
    fun pendingExpiresOnTimeWhileInBackoff() = runTest {
        with(RelayTestKit()) {
            network.online = true
            uploader.behaviour = { UploadResult.Failed }
            val r = relay()
            hear(heard(seal(1)))
            // Retries land at 615 + 300k s, so 43 200 s is not a retry: only the expiry wakes the worker.
            at(43_199_999)
            assertEquals(1, r.pendingCount.value)
            at(43_200_000)
            assertEquals(0, r.pendingCount.value)
            val calls = uploader.calls.size
            at(44_000_000)
            assertEquals(calls, uploader.calls.size)
        }
    }

    @Test
    fun seenExpiresAfter12Hours() = runTest {
        with(RelayTestKit()) {
            network.online = true
            relay()
            hear(heard(seal(1)))
            assertEquals(1, uploader.calls.size)
            at(43_199_999)
            hear(heard(seal(1)))
            assertEquals(1, uploader.calls.size)
            assertEquals(listOf(ACK, ACK), mesh.sent)
            at(43_200_000)
            hear(heard(seal(1)))
            assertEquals(2, uploader.calls.size)
        }
    }

    @Test
    fun expiredEntriesDroppedAtLoad() = runTest {
        with(RelayTestKit()) {
            val old = T0 - 43_200_000
            stored(
                RelaySnapshot(
                    pending = listOf(PendingUpload(seal(1), 0, old, nextAttemptAtMs = old)),
                    seen = listOf(SeenText(relayHash(seal(1)), old)),
                ),
            )
            network.online = true
            val r = relay()
            assertTrue(uploader.calls.isEmpty())
            assertEquals(0, r.pendingCount.value)
            hear(heard(seal(1)))
            assertEquals(listOf(seal(1) to 0), uploader.calls)
        }
    }

    @Test
    fun loadRestoresAndPrunesBeforeStartReturns() = runTest {
        with(RelayTestKit()) {
            val old = T0 - 43_200_000
            val fresh = T0 - 1_000
            stored(
                RelaySnapshot(
                    pending = listOf(
                        PendingUpload(seal(1), 0, old, nextAttemptAtMs = old),
                        PendingUpload(seal(2), 1, fresh, nextAttemptAtMs = fresh),
                    ),
                    seen = listOf(SeenText(relayHash(seal(1)), old), SeenText(relayHash(seal(2)), fresh)),
                    relayedCount = 3,
                ),
            )
            val r = MeshRelay(mesh, uploader, network, store, backgroundScope, { T0 + testScheduler.currentTime })
            r.start()
            // No runCurrent(): the worker has not run, so only the load can have pruned.
            assertEquals(1, mesh.incoming.subscriptionCount.value)
            assertEquals(1, r.pendingCount.value)
            assertEquals(3, r.relayedCount.value)
            val s = saved()!!
            assertEquals(listOf(seal(2)), s.pending.map { it.text })
            assertEquals(listOf(relayHash(seal(2))), s.seen.map { it.hash })

            network.online = true
            r.onNetworkAvailable()
            runCurrent()
            assertEquals(listOf(seal(2) to 1), uploader.calls)
            assertEquals(4, r.relayedCount.value)
        }
    }

    @Test
    fun restoredQueueUploadsOldestHeardFirst() = runTest {
        with(RelayTestKit()) {
            stored(
                RelaySnapshot(
                    pending = listOf(
                        PendingUpload(seal(2), 0, T0 - 1_000, nextAttemptAtMs = T0 - 1_000),
                        PendingUpload(seal(1), 0, T0 - 2_000, nextAttemptAtMs = T0 - 2_000),
                    ),
                ),
            )
            network.online = true
            relay()
            assertEquals(listOf(seal(1), seal(2)), uploader.calls.map { it.first })
        }
    }

    @Test
    fun queueBoundedTo100DropsOldest() = runTest {
        with(RelayTestKit()) {
            val r = relay()
            for (n in 1..101) {
                at((n - 1).toLong())
                hear(heard(seal(n)))
            }
            assertEquals(100, r.pendingCount.value)
            network.online = true
            r.onNetworkAvailable()
            runCurrent()
            assertEquals((2..101).map(::seal), uploader.calls.map { it.first })
        }
    }

    @Test
    fun restoreTruncationForgetsTheDroppedTexts() = runTest {
        with(RelayTestKit()) {
            val texts = (0..RelayLimits.MAX_PENDING).map(::seal)
            stored(
                RelaySnapshot(
                    pending = texts.mapIndexed { i, t -> PendingUpload(t, 0, T0 - 1_000 + i, nextAttemptAtMs = T0 - 1_000 + i) },
                    seen = texts.mapIndexed { i, t -> SeenText(relayHash(t), T0 - 1_000 + i) },
                ),
            )
            val r = relay()
            assertEquals(RelayLimits.MAX_PENDING, r.pendingCount.value)
            val s = saved()!!
            assertEquals(seal(1), s.pending.first().text)
            assertEquals(texts.drop(1).map(::relayHash), s.seen.map { it.hash })
            // The text the restore dropped is not blocked for 12 h: hearing it again queues and uploads it.
            hear(heard(seal(0)))
            assertEquals(seal(0), saved()!!.pending.last().text)
            network.online = true
            r.onNetworkAvailable()
            runCurrent()
            assertEquals(seal(0), uploader.calls.last().first)
            assertEquals(RelayLimits.MAX_PENDING, r.relayedCount.value)
        }
    }

    @Test
    fun restoreTruncationKeepsAStoredAck() = runTest {
        with(RelayTestKit()) {
            val texts = (0..RelayLimits.MAX_PENDING).map(::seal)
            stored(
                RelaySnapshot(
                    pending = texts.mapIndexed { i, t -> PendingUpload(t, 0, T0 - 1_000 + i, nextAttemptAtMs = T0 - 1_000 + i) },
                    seen = listOf(SeenText(relayHash(seal(0)), T0 - 1_000, ACK)),
                ),
            )
            relay()
            // A delivered text's seen entry never goes with a dropped pending item: its ack is still repeated.
            assertEquals(ACK, saved()!!.seen.single().ackText)
            hear(heard(seal(0)))
            assertEquals(listOf(ACK), mesh.sent)
            assertTrue(saved()!!.pending.none { it.text == seal(0) })
        }
    }

    @Test
    fun seenSetBoundedTo500() = runTest {
        with(RelayTestKit()) {
            // Online: only deliveries fill the seen-set past the queue (a dropped pending text leaves it).
            network.online = true
            relay()
            for (n in 0..500) {
                at(n.toLong())
                hear(heard(distinct(n)))
            }
            val seen = saved()!!.seen.map { it.hash }
            assertEquals(500, seen.size)
            assertFalse(relayHash(distinct(0)) in seen)
            assertTrue(relayHash(distinct(1)) in seen)
            assertTrue(relayHash(distinct(500)) in seen)
        }
    }

    @Test
    fun deliveryAfterSeenEvictionKeepsItsSeenEntry() = runTest {
        val target = distinct(1_000)
        val first = RelayTestKit()
        with(first) {
            network.online = true
            var deliver = false
            uploader.behaviour = { t ->
                when {
                    t != target -> UploadResult.Acked(ACK)
                    deliver -> UploadResult.Acked(otherAck)
                    else -> UploadResult.Failed
                }
            }
            val scope = CoroutineScope(backgroundScope.coroutineContext + Job())
            val r = relay(scope)
            hear(heard(target))
            // 501 other texts are delivered while the target waits for its retry at 5 s.
            for (n in 0..500) {
                at(1L + n)
                hear(heard(distinct(n)))
            }
            assertEquals(1, r.pendingCount.value)
            assertFalse(relayHash(target) in saved()!!.seen.map { it.hash })
            deliver = true
            at(5_000)
            assertEquals(502, r.relayedCount.value)
            assertTrue(relayHash(target) in saved()!!.seen.map { it.hash })
            at(20_000)
            hear(heard(target))
            assertEquals(2, uploader.calls.count { it.first == target })
            assertEquals(502, r.relayedCount.value)
            assertEquals(2, mesh.sent.count { it == otherAck })
            scope.cancel()
            runCurrent()
        }
        // After a restart sorts the seen-set by time, the next new text must not evict the delivery's entry.
        with(RelayTestKit(first.store)) {
            network.online = true
            val r = relay()
            hear(heard(distinct(2_000)))
            at(40_000)
            hear(heard(target))
            assertTrue(uploader.calls.none { it.first == target })
            assertEquals(503, r.relayedCount.value)
            assertEquals(listOf(ACK, otherAck), mesh.sent)
        }
    }

    @Test
    fun reheardPendingTextTakesNoSecondSlot() = runTest {
        with(RelayTestKit()) {
            val target = distinct(1_000)
            network.online = true
            uploader.behaviour = { t -> if (t == target) UploadResult.Failed else UploadResult.Acked(ACK) }
            val r = relay()
            hear(heard(target))
            for (n in 0..500) {
                at(1L + n)
                hear(heard(distinct(n)))
            }
            assertFalse(relayHash(target) in saved()!!.seen.map { it.hash })
            at(1_000)
            hear(heard(target))
            assertEquals(1, r.pendingCount.value)
            assertEquals(1, saved()!!.pending.count { it.text == target })
            assertEquals(1, uploader.calls.count { it.first == target })
        }
    }

    @Test
    fun newEntryStampedEarlierIsKeptWhenFull() = runTest {
        with(RelayTestKit()) {
            var clock = T0 + 1_000_000
            val r = MeshRelay(mesh, uploader, network, store, backgroundScope, { clock })
            r.start()
            runCurrent()
            // Online, 401 deliveries; then offline, the queue fills to 99 and the seen-set to 500.
            network.online = true
            for (n in 0..400) hear(heard(distinct(n)))
            network.online = false
            for (n in 401..499) hear(heard(distinct(n)))
            // The clock steps back: the next texts are stamped earlier than everything kept.
            clock = T0
            hear(heard(distinct(500)))
            hear(heard(distinct(501)))
            val s = saved()!!
            assertEquals(100, s.pending.size)
            assertEquals(listOf(distinct(500), distinct(501)), s.pending.takeLast(2).map { it.text })
            assertFalse(s.pending.any { it.text == distinct(401) })
            val seen = s.seen.map { it.hash }
            assertEquals(500, seen.size)
            assertTrue(relayHash(distinct(501)) in seen)
            assertFalse(relayHash(distinct(401)) in seen)
            assertTrue(relayHash(distinct(500)) in seen)
            assertFalse(relayHash(distinct(0)) in seen)
        }
    }

    @Test
    fun corruptOrOldStoreStartsEmpty() = runTest {
        for (json in listOf("{", "null", """{"v":2,"pending":[],"seen":[],"relayedCount":9}""")) {
            with(RelayTestKit()) {
                store.json = json
                network.online = true
                val r = relay()
                assertEquals(json, 0, r.relayedCount.value)
                hear(heard(seal(1)))
                assertEquals(json, listOf(seal(1) to 0), uploader.calls)
                assertEquals(json, 1, r.relayedCount.value)
                assertEquals(json, 1, saved()!!.v)
            }
        }
    }

    @Test
    fun throwingStoreLoadStartsEmpty() = runTest {
        with(RelayTestKit()) {
            val failing = object : RelayStore {
                var json: String? = null

                override fun load(): String? = throw IllegalStateException("disk")

                override fun save(json: String) {
                    this.json = json
                }
            }
            network.online = true
            val r = MeshRelay(mesh, uploader, network, failing, backgroundScope, { T0 + testScheduler.currentTime })
            r.start()
            runCurrent()
            assertEquals(0, r.relayedCount.value)
            hear(heard(seal(1)))
            assertEquals(listOf(seal(1) to 0), uploader.calls)
            assertEquals(1, r.relayedCount.value)
            assertTrue(failing.json!!.contains("\"relayedCount\":1"))
        }
    }
}
