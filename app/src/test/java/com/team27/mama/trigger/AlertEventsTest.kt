package com.team27.mama.trigger

import com.team27.mama.contract.AlertEvent
import com.team27.mama.contract.ContractJson
import com.team27.mama.contract.LocationUpdate
import com.team27.mama.contract.Trigger
import org.junit.Assert.assertEquals
import org.junit.Assert.assertFalse
import org.junit.Assert.assertNull
import org.junit.Assert.assertTrue
import org.junit.Test

class AlertEventsTest {

    private fun body(trigger: Trigger, speedMps: Float?): String {
        val point = AlertEvents.point(10.4961, -66.8531, 12.5f, speedMps, 1_761_296_400_000)
        return AlertEvents.alertBody(AlertEvents.alertEvent("i-1", "d-1", trigger, 1_761_296_400_253, listOf(point)))
    }

    @Test
    fun urlsTrimTheBase() {
        assertEquals("http://127.0.0.1:8080/incidents", AlertEvents.incidentsUrl(" http://127.0.0.1:8080/ "))
        assertEquals("http://h/incidents/abc/points", AlertEvents.pointsUrl("http://h", "abc"))
        assertEquals("https://h/", AlertEvents.rootUrl("https://h//"))
    }

    @Test
    fun pointLeavesOutMissingNegativeOrNaNSpeed() {
        val p = AlertEvents.point(10.5, -66.9, accuracyM = null, speedMps = null, timeMs = 0)
        assertNull(p.accuracyM)
        assertNull(p.speedMps)
        assertEquals("1970-01-01T00:00:00Z", p.recordedAt)
        assertNull(AlertEvents.point(0.0, 0.0, 3f, -1f, 0).speedMps)
        assertNull(AlertEvents.point(0.0, 0.0, 3f, Float.NaN, 0).speedMps)
        assertEquals(0.5, AlertEvents.point(0.0, 0.0, 3f, 0.5f, 0).speedMps!!, 1e-6)
        // A known 0 (still) is a real speed and is sent.
        assertEquals(0.0, AlertEvents.point(0.0, 0.0, 3f, 0f, 0).speedMps!!, 0.0)
    }

    @Test
    fun anUnknownSpeedIsLeftOutOfTheBodyNotSentAsZeroOrNull() {
        val unknown = body(Trigger.VOLUME_PATTERN, speedMps = null)
        assertFalse(unknown.contains("speed_mps"))
        assertTrue(body(Trigger.VOLUME_PATTERN, speedMps = 2.5f).contains("\"speed_mps\":2.5"))
    }

    @Test
    fun alertBodyIsTheFrozenAlertEvent() {
        val event = ContractJson.decodeFromString(AlertEvent.serializer(), body(Trigger.WIDGET, null))
        assertEquals("i-1", event.incidentId)
        assertEquals("d-1", event.deviceId)
        assertEquals(Trigger.WIDGET, event.trigger)
        assertEquals("2025-10-24T09:00:00.253Z", event.triggeredAt)
        assertEquals(1, event.points.size)
        assertEquals(12.5, event.points.single().accuracyM!!, 1e-6)
        assertFalse(event.hasAudio)
    }

    @Test
    fun eachTriggerSendsItsContractValue() {
        assertTrue(body(Trigger.VOLUME_PATTERN, null).contains("\"trigger\":\"volume_pattern\""))
        assertTrue(body(Trigger.WIDGET, null).contains("\"trigger\":\"widget\""))
        assertTrue(body(Trigger.IN_APP, null).contains("\"trigger\":\"in_app\""))
    }

    @Test
    fun pointsBodyIsTheFrozenLocationUpdate() {
        val point = AlertEvents.point(1.0, 2.0, 5f, 8f, 0)
        val update = ContractJson.decodeFromString(LocationUpdate.serializer(), AlertEvents.pointsBody("i", "d", listOf(point)))
        assertEquals(LocationUpdate("i", "d", listOf(point)), update)
    }

    @Test
    fun onlyARecentPreciseFixIsSentAtOnce() {
        assertTrue(AlertEvents.isFreshEnough(ageMs = 2_000, accuracyM = 8f))
        assertTrue(AlertEvents.isFreshEnough(ageMs = 30_000, accuracyM = 50f))
        // Seen on the Xiaomi: a 230 s old fix, and a 4062 m one.
        assertFalse(AlertEvents.isFreshEnough(ageMs = 230_000, accuracyM = 100f))
        assertFalse(AlertEvents.isFreshEnough(ageMs = 1_000, accuracyM = 4062f))
        assertFalse(AlertEvents.isFreshEnough(ageMs = 1_000, accuracyM = null))
        assertFalse(AlertEvents.isFreshEnough(ageMs = -1, accuracyM = 5f))
    }

    @Test
    fun onlyAGpsGradeFixIsPrecise() {
        assertTrue(AlertEvents.isPrecise(1.0f))
        assertTrue(AlertEvents.isPrecise(20f))
        assertFalse(AlertEvents.isPrecise(117f))
        assertFalse(AlertEvents.isPrecise(-1f))
    }

    @Test
    fun aFollowUpFixMustBeNewerAndUsableOrBetter() {
        val gps = FixStamp(elapsedNanos = 1_000, accuracyM = 10f)
        // Newer and usable: sent.
        assertTrue(AlertEvents.worthSending(FixStamp(2_000, 12f), gps))
        // Not newer: never resent.
        assertFalse(AlertEvents.worthSending(FixStamp(1_000, 5f), gps))
        assertFalse(AlertEvents.worthSending(FixStamp(500, 5f), gps))
        // A 4 km cell fix after a GPS one is noise.
        assertFalse(AlertEvents.worthSending(FixStamp(2_000, 4062f), gps))
        // Unknown accuracy is never sent as a follow-up.
        assertFalse(AlertEvents.worthSending(FixStamp(2_000, null), gps))
        // After a coarse first point, anything better counts, even if still coarse.
        val coarse = FixStamp(1_000, 900f)
        assertTrue(AlertEvents.worthSending(FixStamp(2_000, 300f), coarse))
        assertFalse(AlertEvents.worthSending(FixStamp(2_000, 1_200f), coarse))
        assertTrue(AlertEvents.worthSending(FixStamp(2_000, 300f), FixStamp(1_000, null)))
    }
}
