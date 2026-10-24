package com.team27.mama.trigger

import com.team27.mama.contract.Branch

/**
 * The seam the in-app call (#96) plugs into. When the server answers an
 * alert with branch `call`, the trigger calls [ring] once with the incident
 * id, from a background thread, with the phone possibly locked.
 *
 * #96 implements it with the CallStyle incoming-call notification (full
 * screen when locked) that opens #94's "Llamada entrante", and swaps its
 * implementation in at `AppGraph.incomingCall`. Until then the default,
 * [ConfirmationScreenLauncher], opens the trigger confirmation screen.
 */
fun interface IncomingCallLauncher {
    fun ring(incidentId: String)
}

/** What the phone shows once the server has answered the alert. */
sealed interface Handoff {
    /** Branch `call`: "Mamá" calls inside the app ([IncomingCallLauncher]). */
    data class IncomingCall(val incidentId: String) : Handoff

    /** Branch `immediate_alert` or `silent_tracker` (or an unreadable 2xx): the confirmation screen. */
    data class Confirmation(val incidentId: String) : Handoff

    /** The server never took the alert: nothing claims it was sent. */
    data object None : Handoff
}

/** Picks the hand-off for [outcome]. */
fun handoffFor(incidentId: String, outcome: AlertOutcome): Handoff = when (outcome) {
    is AlertOutcome.Accepted -> when (outcome.response?.branch) {
        Branch.CALL -> Handoff.IncomingCall(incidentId)
        Branch.IMMEDIATE_ALERT, Branch.SILENT_TRACKER, null -> Handoff.Confirmation(incidentId)
    }
    is AlertOutcome.Rejected, is AlertOutcome.GaveUp -> Handoff.None
}

/** Picks the hand-off for [outcome] and runs it: [incomingCall] on `call`, [showConfirmation] on the other branches. */
fun handOff(
    incidentId: String,
    outcome: AlertOutcome,
    incomingCall: IncomingCallLauncher,
    showConfirmation: (String) -> Unit,
): Handoff = handoffFor(incidentId, outcome).also {
    when (it) {
        is Handoff.IncomingCall -> incomingCall.ring(it.incidentId)
        is Handoff.Confirmation -> showConfirmation(it.incidentId)
        Handoff.None -> Unit
    }
}
