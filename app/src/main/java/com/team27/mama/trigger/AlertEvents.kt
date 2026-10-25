package com.team27.mama.trigger

import com.team27.mama.contract.AlertEvent
import com.team27.mama.contract.ContractJson
import com.team27.mama.contract.DeviceState
import com.team27.mama.contract.LocationUpdate
import com.team27.mama.contract.Point
import com.team27.mama.contract.Trigger
import java.time.Instant

/** When a fix was taken (elapsedRealtimeNanos) and how precise it is (null: unknown), without Android's Location. */
data class FixStamp(val elapsedNanos: Long, val accuracyM: Float?)

/** The pure part of the trigger: contract bodies, URLs and fix rules, unit-tested without Android. */
object AlertEvents {
    /** A fix this precise places the person well enough to send on its own (meters). */
    const val USABLE_ACCURACY_M = 50f

    fun rootUrl(baseUrl: String) = base(baseUrl) + "/"

    fun incidentsUrl(baseUrl: String) = base(baseUrl) + "/incidents"

    fun pointsUrl(baseUrl: String, incidentId: String) = base(baseUrl) + "/incidents/$incidentId/points"

    /** Good enough to send at once: at most [maxAgeMs] old and [maxAccuracyM] precise (an unknown accuracy is not). */
    fun isFreshEnough(ageMs: Long, accuracyM: Float?, maxAgeMs: Long = 30_000, maxAccuracyM: Float = USABLE_ACCURACY_M): Boolean =
        ageMs in 0..maxAgeMs && accuracyM != null && accuracyM <= maxAccuracyM

    /**
     * The fix's speed if it still says how the person moves now: a fix older
     * than [maxAgeMs] gives null (unknown), so a minutes-old speed never picks
     * the branch. Unknown picks `call`, the branch that asks the person.
     */
    fun currentSpeed(speedMps: Float?, ageMs: Long, maxAgeMs: Long = 30_000): Float? =
        speedMps?.takeIf { ageMs in 0..maxAgeMs }

    /** Precise enough to place the person: GPS-grade, within [maxAccuracyM]. */
    fun isPrecise(accuracyM: Float, maxAccuracyM: Float = 20f): Boolean = accuracyM in 0f..maxAccuracyM

    /**
     * Whether [candidate] is worth a LocationUpdate after [lastSent]: newer,
     * with a known accuracy, and either usable on its own or better than the
     * last one sent. A 4 km cell fix after a 10 m GPS one is noise on the map.
     */
    fun worthSending(candidate: FixStamp, lastSent: FixStamp): Boolean {
        val accuracy = candidate.accuracyM ?: return false
        if (candidate.elapsedNanos <= lastSent.elapsedNanos) return false
        val previous = lastSent.accuracyM
        return accuracy <= USABLE_ACCURACY_M || previous == null || accuracy < previous
    }

    /** A contract Point. A fix without speed or accuracy leaves the field out (never 0); a negative or NaN speed is unknown. */
    fun point(lat: Double, lng: Double, accuracyM: Float?, speedMps: Float?, timeMs: Long) = Point(
        lat = lat,
        lng = lng,
        accuracyM = accuracyM?.toDouble()?.takeIf { it >= 0 },
        speedMps = speedMps?.toDouble()?.takeIf { it >= 0 },
        recordedAt = Instant.ofEpochMilli(timeMs).toString(),
    )

    /**
     * The AlertEvent for one trigger. [incidentId] is made once per trigger
     * and reused by every retry, so the server keeps one incident.
     * [deviceState] is the permission snapshot (#125); null leaves it out.
     */
    fun alertEvent(
        incidentId: String,
        deviceId: String,
        trigger: Trigger,
        triggeredAtMs: Long,
        points: List<Point>,
        deviceState: DeviceState? = null,
    ) = AlertEvent(
        incidentId = incidentId,
        deviceId = deviceId,
        trigger = trigger,
        triggeredAt = Instant.ofEpochMilli(triggeredAtMs).toString(),
        points = points,
        // No audio is recorded or uploaded yet.
        hasAudio = false,
        deviceState = deviceState,
    )

    fun alertBody(event: AlertEvent): String = ContractJson.encodeToString(AlertEvent.serializer(), event)

    fun pointsBody(incidentId: String, deviceId: String, points: List<Point>): String =
        ContractJson.encodeToString(LocationUpdate.serializer(), LocationUpdate(incidentId, deviceId, points))

    private fun base(baseUrl: String) = baseUrl.trim().trimEnd('/')
}
