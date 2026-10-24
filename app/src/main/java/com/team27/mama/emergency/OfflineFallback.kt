package com.team27.mama.emergency

import kotlinx.coroutines.flow.first
import kotlinx.coroutines.withTimeoutOrNull

/**
 * How the live trigger (the #10 service today, #16 after it) hands off to the offline emitter.
 * The trigger posts its AlertEvent first. Only when that alert did not reach the server does it
 * call `graph.emergency?.declare()`, so a phone with internet never opens a second emergency
 * through `POST /emergencies`. Then the trigger's foreground service stays up while the
 * emergency is still being delivered. Without it Android may freeze the process and stop the
 * SMS schedule and the mesh re-emits.
 */
object OfflineFallback {
    /** Upper bound on holding the trigger's foreground service for one emergency. */
    const val HOLD_MAX_MS = 10 * 60_000L

    /** Phases still emitting a frame. NO_CHANNEL keeps ticking until a channel comes back. */
    private val DELIVERING = setOf(
        EmergencyPhase.SENDING,
        EmergencyPhase.SEARCHING_NEARBY,
        EmergencyPhase.NO_CHANNEL,
        EmergencyPhase.CANCELLING,
    )

    /** [alertStatus] is the AlertEvent's HTTP status, -1 on a network error, null when no alert was sent. */
    fun shouldDeclare(alertStatus: Int?): Boolean = alertStatus == null || alertStatus !in 200..299

    /** Suspends while [emergency] is delivering, for at most [maxMs]. */
    suspend fun awaitDelivered(emergency: EmergencyControls, maxMs: Long = HOLD_MAX_MS) {
        withTimeoutOrNull(maxMs) { emergency.state.first { it.phase !in DELIVERING } }
    }
}
