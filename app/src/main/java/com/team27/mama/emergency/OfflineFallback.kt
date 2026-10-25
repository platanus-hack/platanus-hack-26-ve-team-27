package com.team27.mama.emergency

import kotlinx.coroutines.flow.first
import kotlinx.coroutines.withTimeoutOrNull

/**
 * How the live trigger (#16's `trigger.TriggerService`) hands off to the offline emitter.
 * The trigger posts its AlertEvent first. Only when that alert did not reach the server does it
 * call `graph.emergency?.declare()`, so a phone with internet never opens a second emergency
 * through `POST /emergencies`. Then the trigger's foreground service stays up while the
 * emergency is still being delivered. Without it Android may freeze the process and stop the
 * SMS schedule and the mesh re-emits.
 */
object OfflineFallback {
    /** Upper bound on holding the trigger's foreground service for one emergency. */
    const val HOLD_MAX_MS = 10 * 60_000L

    /**
     * Phases still emitting the alert. NO_CHANNEL keeps ticking until a channel comes back.
     * CANCELLING releases the hold: TriggerService ignores a trigger while its incident runs, so
     * holding through the cancel window (up to 120 s offline) would drop a real alert right after
     * a false one. The cancel is tapped in the app, which is then in the foreground, and #60's
     * relay service keeps the mesh on while the cancel goes out (`keepsMeshOn`).
     */
    private val DELIVERING = setOf(
        EmergencyPhase.SENDING,
        EmergencyPhase.SEARCHING_NEARBY,
        EmergencyPhase.NO_CHANNEL,
    )

    /** [alertStatus] is the AlertEvent's HTTP status, -1 on a network error, null when no alert was sent. */
    fun shouldDeclare(alertStatus: Int?): Boolean = alertStatus == null || alertStatus !in 200..299

    /**
     * #61's demo override: a forced channel without HTTP skips the AlertEvent too, so a phone that
     * still has internet goes straight to the offline emergency. So does a phone with no network
     * at all: the AlertEvent cannot leave it, and SMS and the mesh start at once instead of after
     * the alert's retries. Never with the flag off.
     */
    fun skipsOnlineAlert(override: ChannelOverride, emergencyEnabled: Boolean, hasNetwork: Boolean = true): Boolean =
        emergencyEnabled && (!override.allowsHttp || !hasNetwork)

    /**
     * The AlertEvent's retry delays. With the flag the first failed attempt hands off at once: the
     * offline emitter keeps retrying HTTP itself, next to SMS and the mesh, so retrying here only
     * delays them (about 105 s with AlertSender's defaults).
     */
    fun alertRetryDelaysMs(emergencyEnabled: Boolean, retries: List<Long>): List<Long> =
        if (emergencyEnabled) emptyList() else retries

    /** Suspends while [emergency] is delivering, for at most [maxMs]. */
    suspend fun awaitDelivered(emergency: EmergencyControls, maxMs: Long = HOLD_MAX_MS) {
        withTimeoutOrNull(maxMs) { emergency.state.first { it.phase !in DELIVERING } }
    }
}
