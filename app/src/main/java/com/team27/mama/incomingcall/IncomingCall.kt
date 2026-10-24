package com.team27.mama.incomingcall

import com.team27.mama.trigger.IncomingCallLauncher
import com.team27.mama.trigger.TriggerScreens
import com.team27.mama.ui.navigation.Route

/*
 * Mamá's incoming call (#96), the Android-free part: the intents the call
 * notification sends, the screen each one opens, and when the ring stops.
 * NotificationIncomingCallLauncher, IncomingCallReceiver and
 * IncomingCallWindow are the Android adapters around it.
 *
 * Bystander rule: everything here can show on a lock screen, so it reads as
 * an ordinary call from "Mamá". No alert or emergency words.
 */

/** Who calls, what the notification says, and the channel name in Settings. */
internal object CallCopy {
    const val CALLER = "Mamá"
    const val TEXT = "Llamada entrante"
    const val CHANNEL = "Llamadas"
}

/** The screen that rings (tap or full-screen launch). TODO(#94): Route.IncomingCall once #113 is on main. */
internal val RINGING_ROUTE = Route.TriggerConfirmation

/** The screen Contestar opens. TODO(#94): Route.InCall once #113 is on main. */
internal val ANSWERED_ROUTE = Route.TriggerConfirmation

/** The screens that belong to the call: only these show over the lock screen. */
internal val CALL_ROUTES: Set<String> = setOf(RINGING_ROUTE.path, ANSWERED_ROUTE.path)

/** The call notification's three intents. */
enum class CallAction(val action: String) {
    /** Tapping the notification, or its full-screen launch on a locked phone: the ringing screen, still ringing. */
    Ring("com.team27.mama.action.INCOMING_CALL"),

    /** Contestar: straight to the call. The ring stops. */
    Answer("com.team27.mama.action.ANSWER_CALL"),

    /** Rechazar: a broadcast that opens nothing. The ring stops and the incident is cancelled. */
    Decline("com.team27.mama.action.DECLINE_CALL"),
    ;

    companion object {
        fun from(action: String?): CallAction? = entries.firstOrNull { it.action == action }
    }
}

/** One call intent: what MainActivity or IncomingCallReceiver gets. */
data class CallIntent(val action: CallAction, val incidentId: String) {
    /** The screen it opens, or null for Rechazar. */
    val route: Route?
        get() = when (action) {
            CallAction.Ring -> RINGING_ROUTE
            CallAction.Answer -> ANSWERED_ROUTE
            CallAction.Decline -> null
        }

    /**
     * Its extras: the same "route" and "incident_id" the trigger (#16) opens
     * screens with. MainActivity picks its start screen from the route.
     */
    fun extras(): Map<String, String> = buildMap {
        put(TriggerScreens.EXTRA_INCIDENT_ID, incidentId)
        route?.let { put(TriggerScreens.EXTRA_ROUTE, it.path) }
    }

    companion object {
        /** Reads an intent's action and incident id. Null when it is not a call intent. */
        fun parse(action: String?, incidentId: String?): CallIntent? {
            val callAction = CallAction.from(action) ?: return null
            if (incidentId.isNullOrBlank()) return null
            return CallIntent(callAction, incidentId)
        }
    }
}

/** The ringing call: [ring] posts it (full screen when locked), [stop] removes it and the ring with it. */
interface Ringer : IncomingCallLauncher {
    /** Safe to call twice, or with nothing ringing. */
    fun stop()
}

/**
 * What each call intent does to the ring. [onDecline] cancels the incident,
 * the same as Rechazar and Colgar inside the app (#94's onCancelIncident).
 */
class IncomingCallFlow(private val ringer: Ringer, private val onDecline: (incidentId: String) -> Unit) {
    fun handle(intent: CallIntent) {
        when (intent.action) {
            // The ringing screen keeps ringing until it is answered, rejected or times out.
            CallAction.Ring -> Unit
            CallAction.Answer -> ringer.stop()
            CallAction.Decline -> {
                ringer.stop()
                onDecline(intent.incidentId)
            }
        }
    }
}

/**
 * Follows the app's screens once a call intent opened it. The window shows
 * over the lock screen only on a call screen, so a finished call never leaves
 * the rest of the app readable without unlocking. Leaving the ringing screen
 * (Contestar or Rechazar inside the app) stops the ring.
 */
class CallScreenFollower(
    private val ringer: Ringer,
    private val showOverLockScreen: (Boolean) -> Unit,
) {
    private var ringingShown = false

    fun onScreen(route: String?) {
        showOverLockScreen(route in CALL_ROUTES)
        if (route == RINGING_ROUTE.path) {
            ringingShown = true
        } else if (ringingShown) {
            ringingShown = false
            ringer.stop()
        }
    }
}
