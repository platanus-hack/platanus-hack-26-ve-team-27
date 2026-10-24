package com.team27.mama.incomingcall

import android.app.Activity
import android.content.Intent
import android.os.Build
import android.view.WindowManager
import androidx.compose.runtime.Composable
import androidx.compose.runtime.DisposableEffect
import androidx.navigation.NavController
import com.team27.mama.trigger.TriggerScreens

/**
 * MainActivity's side of Mamá's call (#96). [open] runs in onCreate, before
 * the first frame: a call intent puts the activity over the lock screen and
 * turns the screen on, and Contestar stops the ring. [Follow] then keeps the
 * window over the lock screen only on the call screens, and stops the ring
 * when the app leaves the ringing screen.
 */
class IncomingCallWindow private constructor(activity: Activity, ringer: Ringer) {
    private val follower = CallScreenFollower(ringer) { showOverLockScreen(activity, it) }

    @Composable
    fun Follow(navController: NavController) {
        DisposableEffect(navController) {
            val listener = NavController.OnDestinationChangedListener { _, destination, _ ->
                follower.onScreen(destination.route)
            }
            navController.addOnDestinationChangedListener(listener)
            onDispose { navController.removeOnDestinationChangedListener(listener) }
        }
    }

    companion object {
        /** Null unless the call notification opened the activity with [intent]. */
        fun open(activity: Activity, intent: Intent): IncomingCallWindow? {
            val call = CallIntent.parse(intent.action, intent.getStringExtra(TriggerScreens.EXTRA_INCIDENT_ID))
                ?: return null
            val ringer = NotificationIncomingCallLauncher(activity)
            showOverLockScreen(activity, true)
            IncomingCallFlow(ringer) { cancelIncident(activity, it) }.handle(call)
            return IncomingCallWindow(activity, ringer)
        }

        private fun showOverLockScreen(activity: Activity, on: Boolean) {
            if (Build.VERSION.SDK_INT >= 27) {
                activity.setShowWhenLocked(on)
                activity.setTurnScreenOn(on)
            } else {
                @Suppress("DEPRECATION")
                val flags = WindowManager.LayoutParams.FLAG_SHOW_WHEN_LOCKED or WindowManager.LayoutParams.FLAG_TURN_SCREEN_ON
                if (on) activity.window.addFlags(flags) else activity.window.clearFlags(flags)
            }
        }
    }
}
