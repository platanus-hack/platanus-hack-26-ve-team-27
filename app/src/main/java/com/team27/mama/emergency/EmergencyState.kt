package com.team27.mama.emergency

import kotlinx.coroutines.flow.StateFlow

enum class EmergencyPhase {
    IDLE,
    SENDING,
    SEARCHING_NEARBY,
    NOTIFIED,
    NO_CHANNEL,
    CANCELLING,
    CANCELLED,
    ;

    /** An emergency is in progress, its cancel included. A new one is refused only before CANCELLING. */
    val isActive: Boolean get() = this != IDLE && this != CANCELLED
}

enum class Channel { HTTP, SMS, MESH }

enum class MeshIssue {
    /** The server keys were never fetched: nothing can be sealed for the mesh. */
    NO_SERVER_KEYS,

    /** Bluetooth is off, a permission is missing or the mesh identity is unusable. */
    UNAVAILABLE,
}

data class EmergencyState(
    val phase: EmergencyPhase = EmergencyPhase.IDLE,
    val emergencyId: Long? = null,
    /** seq of the frame being emitted. */
    val seq: Int = 0,
    /** Highest valid ack seq. */
    val ackedSeq: Int? = null,
    val channelsUsed: Set<Channel> = emptySet(),
    val meshIssue: MeshIssue? = null,
)

interface EmergencyControls {
    val state: StateFlow<EmergencyState>

    /**
     * Starts an emergency; false while one is still alerting (SENDING, SEARCHING_NEARBY, NOTIFIED,
     * NO_CHANNEL). During CANCELLING it starts a new emergency with a new id.
     */
    fun declare(): Boolean

    /** No-op when IDLE, CANCELLING or CANCELLED. */
    fun cancel()
}
