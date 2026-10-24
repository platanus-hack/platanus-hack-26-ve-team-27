package com.team27.mama.spike

import com.team27.mama.contract.AlertEvent
import com.team27.mama.contract.ContractJson
import com.team27.mama.contract.LocationUpdate
import com.team27.mama.contract.Trigger
import org.junit.Assert.assertEquals
import org.junit.Assert.assertFalse
import org.junit.Assert.assertNull
import org.junit.Assert.assertTrue
import org.junit.Test

class SpikeEventsTest {

    @Test
    fun urlsTrimTheBase() {
        assertEquals("http://127.0.0.1:8089/incidents", SpikeEvents.incidentsUrl(" http://127.0.0.1:8089/ "))
        assertEquals("http://h/incidents/abc/points", SpikeEvents.pointsUrl("http://h", "abc"))
        assertEquals("http://h/spike/note", SpikeEvents.noteUrl("http://h/"))
    }

    @Test
    fun pointLeavesOutMissingOrNegativeSpeed() {
        val p = SpikeEvents.point(10.5, -66.9, accuracyM = null, speedMps = null, timeMs = 0)
        assertNull(p.accuracyM)
        assertNull(p.speedMps)
        assertEquals("1970-01-01T00:00:00Z", p.recordedAt)
        assertNull(SpikeEvents.point(0.0, 0.0, 3f, -1f, 0).speedMps)
        assertEquals(0.5, SpikeEvents.point(0.0, 0.0, 3f, 0.5f, 0).speedMps!!, 1e-6)
    }

    @Test
    fun alertBodyIsTheFrozenAlertEvent() {
        val point = SpikeEvents.point(10.4961, -66.8531, 12.5f, null, 1_761_296_400_000)
        val body = SpikeEvents.alertBody("i-1", "d-1", Trigger.WIDGET, 1_761_296_400_253, point)
        val event = ContractJson.decodeFromString(AlertEvent.serializer(), body)
        assertEquals(Trigger.WIDGET, event.trigger)
        assertEquals("2025-10-24T09:00:00.253Z", event.triggeredAt)
        assertEquals(listOf(point), event.points)
        assertFalse(event.hasAudio)
        assertFalse("unknown speed is left out, never null", body.contains("speed_mps"))
        assertTrue(SpikeEvents.alertBody("i", "d", Trigger.VOLUME_PATTERN, 0, point).contains("\"trigger\":\"volume_pattern\""))
    }

    @Test
    fun pointsBodyIsTheFrozenLocationUpdate() {
        val point = SpikeEvents.point(1.0, 2.0, 5f, 8f, 0)
        val update = ContractJson.decodeFromString(LocationUpdate.serializer(), SpikeEvents.pointsBody("i", "d", point))
        assertEquals(LocationUpdate("i", "d", listOf(point)), update)
    }

    @Test
    fun peakAmplitudeIsZeroOnlyForSilence() {
        assertEquals(0, SpikeEvents.peakAmplitude(ShortArray(100), 100))
        assertEquals(32768, SpikeEvents.peakAmplitude(shortArrayOf(3, Short.MIN_VALUE, 7), 3))
        assertEquals(3, SpikeEvents.peakAmplitude(shortArrayOf(3, -9), 1))
        assertEquals(9, SpikeEvents.peakAmplitude(shortArrayOf(3, -9), 50))
    }

    @Test
    fun onlyARecentPreciseFixIsSentAtOnce() {
        assertTrue(SpikeEvents.isFreshEnough(ageMs = 2_000, accuracyM = 8f))
        assertTrue(SpikeEvents.isFreshEnough(ageMs = 30_000, accuracyM = 50f))
        // Seen on the Xiaomi: a 230 s old fix, and a 4062 m one.
        assertFalse(SpikeEvents.isFreshEnough(ageMs = 230_000, accuracyM = 100f))
        assertFalse(SpikeEvents.isFreshEnough(ageMs = 1_000, accuracyM = 4062f))
        assertFalse(SpikeEvents.isFreshEnough(ageMs = 1_000, accuracyM = null))
        assertFalse(SpikeEvents.isFreshEnough(ageMs = -1, accuracyM = 5f))
    }

    @Test
    fun videoUrlCarriesTheIncident() {
        assertEquals("http://h/spike/video?incident_id=abc", SpikeEvents.videoUrl("http://h/", "abc"))
    }

    @Test
    fun onlyAGpsGradeFixIsPrecise() {
        assertTrue(SpikeEvents.isPrecise(1.0f))
        assertTrue(SpikeEvents.isPrecise(20f))
        assertFalse(SpikeEvents.isPrecise(117f))
        assertFalse(SpikeEvents.isPrecise(-1f))
    }
}
