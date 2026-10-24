package com.team27.mama.debug

import android.content.BroadcastReceiver
import android.content.Context
import android.content.Intent
import android.util.Log
import com.team27.mama.MamaApp
import com.team27.mama.incomingcall.NotificationIncomingCallLauncher

/**
 * Debug-only adb hook for Mamá's incoming call (#96): rings the way the
 * trigger (#16) does on branch `call`, through AppGraph.incomingCall, without
 * a server.
 *
 *     adb shell am broadcast -n com.team27.mama/com.team27.mama.debug.IncomingCallDebugReceiver \
 *         -a com.team27.mama.debug.RING [--es incident_id <id>]
 *     adb shell am broadcast -n com.team27.mama/com.team27.mama.debug.IncomingCallDebugReceiver \
 *         -a com.team27.mama.debug.STOP_RING
 *
 * Like MeshDebugReceiver it requires android.permission.DUMP, which the adb
 * shell holds and other apps do not.
 */
class IncomingCallDebugReceiver : BroadcastReceiver() {
    override fun onReceive(context: Context, intent: Intent) {
        when (intent.action) {
            ACTION_RING -> {
                val incidentId = intent.getStringExtra("incident_id") ?: "debug-call"
                (context.applicationContext as MamaApp).graph.incomingCall.ring(incidentId)
                Log.i(TAG, "ring $incidentId")
            }
            ACTION_STOP -> {
                NotificationIncomingCallLauncher(context).stop()
                Log.i(TAG, "stop")
            }
            else -> Log.w(TAG, "unknown action ${intent.action}")
        }
    }

    private companion object {
        const val TAG = "MamaCallDebug"
        const val ACTION_RING = "com.team27.mama.debug.RING"
        const val ACTION_STOP = "com.team27.mama.debug.STOP_RING"
    }
}
