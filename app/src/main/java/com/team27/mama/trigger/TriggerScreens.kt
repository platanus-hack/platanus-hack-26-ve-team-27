package com.team27.mama.trigger

import android.content.Context
import android.content.Intent
import com.team27.mama.MainActivity
import com.team27.mama.ui.navigation.Route

/**
 * Opens the app on a screen from the trigger's background thread. Android
 * lets the app start an activity from the background while its accessibility
 * service is bound; HyperOS also needs "Display pop-up windows while running
 * in the background" (setup's pop-ups step). With the phone locked the screen
 * waits behind the lock screen.
 */
object TriggerScreens {
    /** MainActivity reads it to pick the start screen; the value is a [Route.path]. */
    const val EXTRA_ROUTE = "route"
    const val EXTRA_INCIDENT_ID = "incident_id"

    fun openConfirmation(context: Context, incidentId: String) = open(context, Route.TriggerConfirmation, incidentId)

    fun open(context: Context, route: Route, incidentId: String) {
        val intent = Intent(context, MainActivity::class.java)
            .putExtra(EXTRA_ROUTE, route.path)
            .putExtra(EXTRA_INCIDENT_ID, incidentId)
            // CLEAR_TOP re-creates a running MainActivity, so it starts on [route].
            .addFlags(Intent.FLAG_ACTIVITY_NEW_TASK or Intent.FLAG_ACTIVITY_CLEAR_TOP)
        try {
            context.startActivity(intent)
            TriggerLog.log(context, "opened ${route.path} for $incidentId")
        } catch (e: Exception) {
            TriggerLog.log(context, "open ${route.path} failed ${e.javaClass.simpleName}: ${e.message}")
        }
    }
}

/**
 * The default [IncomingCallLauncher] until #96: the `call` branch opens the
 * trigger confirmation screen ("Mamá te va a llamar"), like the other branches.
 */
class ConfirmationScreenLauncher(private val context: Context) : IncomingCallLauncher {
    override fun ring(incidentId: String) = TriggerScreens.openConfirmation(context, incidentId)
}
