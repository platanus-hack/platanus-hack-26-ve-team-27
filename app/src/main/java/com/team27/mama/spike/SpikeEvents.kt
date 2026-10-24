package com.team27.mama.spike

import com.team27.mama.contract.AlertEvent
import com.team27.mama.contract.ContractJson
import com.team27.mama.contract.LocationUpdate
import com.team27.mama.contract.Point
import com.team27.mama.contract.Trigger
import java.time.Instant

/** The pure part of the spike: frozen-shape bodies and URLs, unit-tested without Android. */
object SpikeEvents {
    fun incidentsUrl(baseUrl: String) = base(baseUrl) + "/incidents"

    fun pointsUrl(baseUrl: String, incidentId: String) = base(baseUrl) + "/incidents/$incidentId/points"

    /** Spike-only route on spikes/trigger/receiver.go; never on the real server. */
    fun noteUrl(baseUrl: String) = base(baseUrl) + "/spike/note"

    /** A contract Point. A fix without speed or accuracy leaves the field out (never 0). */
    fun point(lat: Double, lng: Double, accuracyM: Float?, speedMps: Float?, timeMs: Long) = Point(
        lat = lat,
        lng = lng,
        accuracyM = accuracyM?.toDouble(),
        speedMps = speedMps?.toDouble()?.takeIf { it >= 0 },
        recordedAt = Instant.ofEpochMilli(timeMs).toString(),
    )

    fun alertBody(incidentId: String, deviceId: String, trigger: Trigger, triggeredAtMs: Long, point: Point): String =
        ContractJson.encodeToString(
            AlertEvent.serializer(),
            AlertEvent(
                incidentId = incidentId,
                deviceId = deviceId,
                trigger = trigger,
                triggeredAt = Instant.ofEpochMilli(triggeredAtMs).toString(),
                points = listOf(point),
                // The spike never uploads audio; the mic probe result goes in the note.
                hasAudio = false,
            ),
        )

    fun pointsBody(incidentId: String, deviceId: String, point: Point): String =
        ContractJson.encodeToString(LocationUpdate.serializer(), LocationUpdate(incidentId, deviceId, listOf(point)))

    /** The loudest absolute sample in the first [count] samples; 0 means the mic was silenced (or a dead-silent room). */
    fun peakAmplitude(samples: ShortArray, count: Int): Int {
        var peak = 0
        for (i in 0 until count.coerceAtMost(samples.size)) {
            val v = kotlin.math.abs(samples[i].toInt())
            if (v > peak) peak = v
        }
        return peak
    }

    private fun base(baseUrl: String) = baseUrl.trim().trimEnd('/')
}
