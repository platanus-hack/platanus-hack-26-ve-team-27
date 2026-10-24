package com.team27.mama.incomingcall

import android.content.BroadcastReceiver
import android.content.Context
import android.content.Intent
import com.team27.mama.trigger.TriggerLog
import com.team27.mama.trigger.TriggerScreens

/** Rechazar on the call notification (#96): stops the ring and cancels the incident. Not exported. */
class IncomingCallReceiver : BroadcastReceiver() {
    override fun onReceive(context: Context, intent: Intent) {
        val call = CallIntent.parse(intent.action, intent.getStringExtra(TriggerScreens.EXTRA_INCIDENT_ID)) ?: return
        IncomingCallFlow(NotificationIncomingCallLauncher(context)) { cancelIncident(context, it) }.handle(call)
    }
}

/**
 * Cancels the incident after Rechazar, the same as Rechazar and Colgar inside
 * the app (#94's onCancelIncident). The server has no endpoint that cancels an
 * incident yet, so for now the call just ends; the ticket that adds one calls
 * it here.
 */
internal fun cancelIncident(context: Context, incidentId: String) {
    TriggerLog.log(context, "call: declined $incidentId")
}
