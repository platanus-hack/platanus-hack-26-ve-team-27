package com.team27.mama.permissions

import com.team27.mama.contract.ContractJson
import com.team27.mama.contract.PermissionEvent
import com.team27.mama.contract.PermissionEvents
import com.team27.mama.contract.PermissionKeys
import com.team27.mama.server.HttpResponse
import com.team27.mama.server.HttpTransport
import com.team27.mama.setup.KeyValueStore
import java.io.IOException
import org.junit.Assert.assertEquals
import org.junit.Assert.assertFalse
import org.junit.Assert.assertTrue
import org.junit.Test

class PermissionSyncTest {

    private class MemoryStore : KeyValueStore {
        private val values = mutableMapOf<String, Any?>()
        override fun getString(key: String) = values[key] as String?
        override fun putString(key: String, value: String?) {
            values[key] = value
        }
        override fun getBoolean(key: String) = values[key] as Boolean? ?: false
        override fun putBoolean(key: String, value: Boolean) {
            values[key] = value
        }
    }

    private class Rig(var state: Map<String, Boolean>, var status: Int = 204, var ready: Boolean = true) {
        val log = PermissionLog(MemoryStore())
        val sent = mutableListOf<List<PermissionEvent>>()
        val errors = mutableListOf<String>()
        var clock = 1_792_828_920_000 // 2026-10-24T08:02:00Z
        val sync = PermissionSync(
            log = log,
            read = { state },
            send = { batch -> sent += batch; status },
            ready = { ready },
            executor = Runnable::run,
            now = { clock++ },
            onError = { errors += it },
        )
    }

    @Test
    fun beforeSetupIsDoneItOnlyRecords() {
        val rig = Rig(mapOf(PermissionKeys.NOTIFICATIONS to true), ready = false)
        rig.sync.sync()
        assertTrue(rig.sent.isEmpty())
        assertEquals(1, rig.log.pending().size)

        rig.ready = true
        rig.state = rig.state + (PermissionKeys.SMS to true)
        rig.sync.sync()
        assertEquals(listOf(listOf(PermissionKeys.NOTIFICATIONS, PermissionKeys.SMS)), rig.sent.map { b -> b.map { it.permission } })
        assertTrue(rig.log.pending().isEmpty())

        // Nothing new: nothing sent.
        rig.sync.sync()
        assertEquals(1, rig.sent.size)
    }

    @Test
    fun aFailedSendKeepsTheEventsForNextTime() {
        for (status in listOf(-1, 422, 500, 503)) {
            val rig = Rig(mapOf(PermissionKeys.SMS to true), status = status)
            rig.sync.sync()
            assertEquals("HTTP $status", 1, rig.log.pending().size)
            assertEquals(1, rig.errors.size)
            rig.status = 204
            assertTrue(rig.sync.flush())
            assertEquals(listOf(rig.sent[0], rig.sent[0]), rig.sent)
        }
    }

    @Test
    fun aBatchTheServerRefusesAsMalformedIsDropped() {
        val rig = Rig(mapOf(PermissionKeys.SMS to true), status = 400)
        rig.sync.sync()
        assertTrue(rig.log.pending().isEmpty())
    }

    @Test
    fun aLongQueueGoesInBatchesTheServerAccepts() {
        val rig = Rig(emptyMap(), ready = false)
        repeat(PermissionLog.MAX_PENDING) { i ->
            rig.state = mapOf(PermissionKeys.SMS to (i % 2 == 0))
            rig.sync.sync()
        }
        rig.ready = true
        assertTrue(rig.sync.flush())
        assertEquals(listOf(PermissionSync.MAX_PER_REQUEST, PermissionSync.MAX_PER_REQUEST), rig.sent.map { it.size })
    }

    @Test
    fun aReadThatThrowsNeverEscapes() {
        val errors = mutableListOf<String>()
        PermissionSync(
            log = PermissionLog(MemoryStore()),
            read = { throw SecurityException("no") },
            send = { 204 },
            ready = { true },
            executor = Runnable::run,
            onError = { errors += it },
        ).sync()
        assertEquals(listOf("permission sync failed SecurityException"), errors)
    }

    @Test
    fun clientPostsTheContractShapeToTheDevicesRoute() {
        var seen: Triple<String, String, String?>? = null
        val client = PermissionEventsClient(" https://mama.example/ ") { method, url, body ->
            seen = Triple(method, url, body)
            HttpResponse(204, "")
        }
        val events = listOf(PermissionEvent(PermissionKeys.SMS, true, "2026-10-24T08:02:00Z"))
        assertEquals(204, client.send("3f1c9a52-7b1e-4c8a-9a0e-2d6b8f4e1a77", events))
        val (method, url, body) = seen!!
        assertEquals("POST", method)
        assertEquals("https://mama.example/devices/3f1c9a52-7b1e-4c8a-9a0e-2d6b8f4e1a77/permissions", url)
        assertEquals(events, ContractJson.decodeFromString(PermissionEvents.serializer(), body!!).events)
    }

    @Test
    fun clientReportsANetworkErrorAsMinusOne() {
        val down = HttpTransport { _, _, _ -> throw IOException("offline") }
        assertEquals(-1, PermissionEventsClient("https://mama.example", down).send("d", emptyList()))
        assertFalse(PermissionEventsClient.body(emptyList()).contains("null"))
    }
}
