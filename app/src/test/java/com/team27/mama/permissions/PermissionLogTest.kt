package com.team27.mama.permissions

import com.team27.mama.contract.PermissionEvent
import com.team27.mama.contract.PermissionKeys
import com.team27.mama.setup.KeyValueStore
import org.junit.Assert.assertEquals
import org.junit.Assert.assertTrue
import org.junit.Test

class PermissionLogTest {

    private class MemoryStore : KeyValueStore {
        val values = mutableMapOf<String, Any?>()
        override fun getString(key: String) = values[key] as String?
        override fun putString(key: String, value: String?) {
            values[key] = value
        }
        override fun getBoolean(key: String) = values[key] as Boolean? ?: false
        override fun putBoolean(key: String, value: Boolean) {
            values[key] = value
        }
    }

    // 2026-10-24T08:02:00Z and one minute later.
    private val t0 = 1_792_828_920_000
    private val t1 = t0 + 60_000

    @Test
    fun theFirstLookQueuesOnlyWhatIsGrantedInSetupOrder() {
        val log = PermissionLog(MemoryStore())
        val events = log.observe(
            mapOf(PermissionKeys.SMS to true, PermissionKeys.MICROPHONE to false, PermissionKeys.NOTIFICATIONS to true),
            t0,
        )
        assertEquals(
            listOf(
                PermissionEvent(PermissionKeys.NOTIFICATIONS, true, "2026-10-24T08:02:00Z"),
                PermissionEvent(PermissionKeys.SMS, true, "2026-10-24T08:02:00Z"),
            ),
            events,
        )
        assertEquals(events, log.pending())
    }

    @Test
    fun setupGrantsOneByOneAndLaterLossesAreEvents() {
        val log = PermissionLog(MemoryStore())
        log.observe(mapOf(PermissionKeys.NOTIFICATIONS to false, PermissionKeys.LOCATION_FINE to false), t0)
        assertTrue(log.pending().isEmpty())

        log.observe(mapOf(PermissionKeys.NOTIFICATIONS to true, PermissionKeys.LOCATION_FINE to false), t0 + 1_000)
        log.observe(mapOf(PermissionKeys.NOTIFICATIONS to true, PermissionKeys.LOCATION_FINE to true), t0 + 2_512)
        // Nothing changed: nothing queued.
        assertTrue(log.observe(mapOf(PermissionKeys.NOTIFICATIONS to true, PermissionKeys.LOCATION_FINE to true), t1).isEmpty())
        // Android took precise location away (hibernation, or the person in Settings).
        log.observe(mapOf(PermissionKeys.NOTIFICATIONS to true, PermissionKeys.LOCATION_FINE to false), t1)

        assertEquals(
            listOf(
                PermissionEvent(PermissionKeys.NOTIFICATIONS, true, "2026-10-24T08:02:01Z"),
                PermissionEvent(PermissionKeys.LOCATION_FINE, true, "2026-10-24T08:02:02.512Z"),
                PermissionEvent(PermissionKeys.LOCATION_FINE, false, "2026-10-24T08:03:00Z"),
            ),
            log.pending(),
        )
    }

    @Test
    fun aPermissionMissingFromASnapshotKeepsItsLastValue() {
        val log = PermissionLog(MemoryStore())
        log.observe(mapOf(PermissionKeys.SMS to true), t0)
        log.confirm(log.pending())
        // A snapshot without sms says nothing about it: no event.
        assertTrue(log.observe(mapOf(PermissionKeys.MICROPHONE to false), t1).isEmpty())
        assertEquals(listOf(PermissionEvent(PermissionKeys.SMS, false, "2026-10-24T08:03:00Z")), log.observe(mapOf(PermissionKeys.SMS to false), t1))
    }

    @Test
    fun confirmDropsWhatTheServerTookAndTheRestSurvivesANewLog() {
        val kv = MemoryStore()
        val log = PermissionLog(kv)
        val first = log.observe(mapOf(PermissionKeys.NOTIFICATIONS to true), t0)
        log.observe(mapOf(PermissionKeys.NOTIFICATIONS to true, PermissionKeys.SMS to true), t1)
        log.confirm(first)
        val reopened = PermissionLog(kv)
        assertEquals(listOf(PermissionEvent(PermissionKeys.SMS, true, "2026-10-24T08:03:00Z")), reopened.pending())
        // It also remembers what it saw: the same snapshot queues nothing.
        assertTrue(reopened.observe(mapOf(PermissionKeys.NOTIFICATIONS to true, PermissionKeys.SMS to true), t1 + 1).isEmpty())
    }

    @Test
    fun theQueueKeepsTheNewestEvents() {
        val log = PermissionLog(MemoryStore(), maxPending = 3)
        repeat(5) { i -> log.observe(mapOf(PermissionKeys.SMS to (i % 2 == 0)), t0 + i * 1_000L) }
        // Granted at 0 s, lost at 1 s, and so on: the last three are kept.
        assertEquals(listOf(true, false, true), log.pending().map { it.granted })
        assertEquals("2026-10-24T08:02:04Z", log.pending().last().at)
    }

    @Test
    fun aCorruptStoreReadsAsEmpty() {
        val kv = MemoryStore()
        kv.putString("pending", "not json")
        kv.putString("last_seen", "[1,2]")
        val log = PermissionLog(kv)
        assertTrue(log.pending().isEmpty())
        assertEquals(1, log.observe(mapOf(PermissionKeys.SMS to true), t0).size)
    }
}
