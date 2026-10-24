package com.team27.mama.contract

import java.io.File
import java.math.BigDecimal
import kotlinx.serialization.KSerializer
import kotlinx.serialization.json.Json
import kotlinx.serialization.json.JsonArray
import kotlinx.serialization.json.JsonElement
import kotlinx.serialization.json.JsonNull
import kotlinx.serialization.json.JsonObject
import kotlinx.serialization.json.JsonPrimitive
import org.junit.Assert.assertEquals
import org.junit.Assert.fail
import org.junit.Test

/**
 * Each canonical sample in server/internal/contract/testdata/ must parse into
 * its Kotlin type with no unknown field and re-emit as the same JSON. The Go
 * TestRoundTripSamples reads the same files.
 */
class ContractRoundTripTest {

    /** Same as [ContractJson] but fails on any field the type does not model. */
    private val strictJson = Json {
        ignoreUnknownKeys = false
        explicitNulls = false
        encodeDefaults = true
        coerceInputValues = true
    }

    private val testdataDir: File by lazy {
        val candidates = listOf(
            File("../server/internal/contract/testdata"),
            File("server/internal/contract/testdata"),
        )
        candidates.firstOrNull { it.isDirectory }
            ?: error(
                "contract testdata not found; tried " +
                    candidates.joinToString { it.absolutePath },
            )
    }

    private fun <T> assertRoundTrip(file: String, serializer: KSerializer<T>) {
        val original = File(testdataDir, file).readText()
        val decoded = try {
            strictJson.decodeFromString(serializer, original)
        } catch (e: Exception) {
            throw AssertionError("$file: decode failed: ${e.message}", e)
        }
        val reemitted = ContractJson.encodeToString(serializer, decoded)
        val want = normalize(Json.parseToJsonElement(original))
        val got = normalize(Json.parseToJsonElement(reemitted))
        assertEquals("$file: re-emitted JSON differs\nwant $original\n got $reemitted", want, got)
    }

    /**
     * Numbers compare by value, so 8 and 8.0 are equal (Go and Kotlin
     * format whole floats differently).
     */
    private fun normalize(e: JsonElement): JsonElement = when (e) {
        is JsonObject -> JsonObject(e.mapValues { normalize(it.value) })
        is JsonArray -> JsonArray(e.map { normalize(it) })
        JsonNull -> e
        is JsonPrimitive ->
            if (e.isString) e
            else e.content.toBigDecimalOrNull()
                ?.let { JsonPrimitive(it.stripTrailingZeros().toPlainString()) }
                ?: e
    }

    @Test fun deviceRegistration() = assertRoundTrip("device_registration.json", DeviceRegistration.serializer())

    @Test fun alertEvent() = assertRoundTrip("alert_event.json", AlertEvent.serializer())

    @Test fun alertResponse() = assertRoundTrip("alert_response.json", AlertResponse.serializer())

    @Test fun locationUpdate() = assertRoundTrip("location_update.json", LocationUpdate.serializer())

    @Test fun callWebhook() = assertRoundTrip("call_webhook.json", CallWebhook.serializer())

    @Test fun notifierMessage() = assertRoundTrip("notifier_message.json", NotifierMessage.serializer())

    @Test
    fun everySampleHasACase() {
        val files = testdataDir.listFiles { f -> f.isFile && f.name.endsWith(".json") }.orEmpty()
        if (files.size != 6) {
            fail("testdata has ${files.size} samples, ContractRoundTripTest covers 6: add the new one here")
        }
    }

    @Test
    fun chooseBranchCases() {
        val cases = listOf(
            Triple("unknown speed", null, Branch.CALL),
            Triple("negative speed", -1.0, Branch.CALL),
            Triple("NaN speed", Double.NaN, Branch.CALL),
            Triple("standing", 0.0, Branch.IMMEDIATE_ALERT),
            Triple("just under still", 0.99, Branch.IMMEDIATE_ALERT),
            Triple("still threshold is a walk", SpeedThresholds.STILL_BELOW_MPS, Branch.CALL),
            Triple("walking", 1.4, Branch.CALL),
            Triple("just under fast", 6.99, Branch.CALL),
            Triple("fast threshold", SpeedThresholds.FAST_FROM_MPS, Branch.SILENT_TRACKER),
            Triple("car", 15.0, Branch.SILENT_TRACKER),
        )
        for ((name, speed, want) in cases) {
            assertEquals(name, want, chooseBranch(speed))
        }
    }

    @Test
    fun alertEventBranchUsesNewestPoint() {
        val e = AlertEvent(
            incidentId = "i",
            deviceId = "d",
            trigger = Trigger.IN_APP,
            triggeredAt = "2026-10-24T09:10:00Z",
            points = listOf(
                Point(lat = 0.0, lng = 0.0, speedMps = 15.0, recordedAt = "2026-10-24T09:10:00Z"),
                Point(lat = 0.0, lng = 0.0, speedMps = 0.2, recordedAt = "2026-10-24T09:10:01Z"),
            ),
            hasAudio = false,
        )
        assertEquals(Branch.IMMEDIATE_ALERT, e.branch())
        assertEquals(Branch.CALL, e.copy(points = emptyList()).branch())
    }

    @Test
    fun callWebhookIncidentId() {
        val w = ContractJson.decodeFromString(
            CallWebhook.serializer(),
            File(testdataDir, "call_webhook.json").readText(),
        )
        assertEquals("9b2e4f1d-3c5a-4e7b-8d6f-1a2b3c4d5e6f", w.incidentId())
    }

    @Test
    fun transcriptNullMessageBecomesEmpty() {
        val t = ContractJson.decodeFromString(
            TranscriptTurn.serializer(),
            """{"role":"user","message":null,"time_in_call_secs":1.5,"extra":true}""",
        )
        assertEquals("", t.message)
    }
}
