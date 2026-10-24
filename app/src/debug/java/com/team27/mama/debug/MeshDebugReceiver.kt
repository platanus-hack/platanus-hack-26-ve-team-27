package com.team27.mama.debug

import android.content.BroadcastReceiver
import android.content.Context
import android.content.Intent
import android.util.Log
import com.team27.mama.mesh.MeshGateway
import kotlinx.coroutines.CoroutineScope
import kotlinx.coroutines.Dispatchers
import kotlinx.coroutines.SupervisorJob
import kotlinx.coroutines.flow.launchIn
import kotlinx.coroutines.flow.onEach

/**
 * Debug-only adb hook for the two-phone mesh test:
 * `adb shell am broadcast -n com.team27.mama/com.team27.mama.debug.MeshDebugReceiver -a <action>`.
 * The receiver requires android.permission.DUMP, which the adb shell holds and other apps do not.
 */
class MeshDebugReceiver : BroadcastReceiver() {

    override fun onReceive(context: Context, intent: Intent) {
        try {
            val gateway = MeshGateway.getInstance(context)
            when (intent.action) {
                ACTION_START -> {
                    // Launch the collector first: broadcasts has no replay.
                    startCollectorOnce(gateway)
                    if (gateway.start()) {
                        Log.i(TAG, "start=true peerId=${gateway.identity.peerId}")
                    } else {
                        Log.w(TAG, "start=false (Bluetooth off or unsupported, permissions or key store)")
                    }
                }
                ACTION_SEND -> {
                    val text = intent.getStringExtra("text")
                    if (text == null) {
                        Log.w(TAG, "send needs string extra 'text'")
                    } else {
                        Log.i(TAG, "send=${gateway.broadcast(text)} text=$text")
                    }
                }
                ACTION_STOP -> {
                    gateway.stop()
                    Log.i(TAG, "stopped")
                }
                else -> Log.w(TAG, "unknown action ${intent.action}")
            }
        } catch (e: Exception) {
            // Never crash the app from an adb hook (bad text, key store or Bluetooth errors).
            Log.w(TAG, "failed: ${e.javaClass.simpleName}: ${e.message}")
        }
    }

    private fun startCollectorOnce(gateway: MeshGateway) {
        synchronized(MeshDebugReceiver) {
            if (collecting) return
            collecting = true
        }
        gateway.broadcasts
            .onEach { Log.i(TAG, "rx text=${it.text} from=${it.senderPeerId} ttl=${it.ttl} relayHops=${it.relayHops}") }
            .launchIn(scope)
    }

    companion object {
        private const val TAG = "MeshDebug"
        const val ACTION_START = "com.team27.mama.debug.MESH_START"
        const val ACTION_SEND = "com.team27.mama.debug.MESH_SEND"
        const val ACTION_STOP = "com.team27.mama.debug.MESH_STOP"

        private val scope = CoroutineScope(SupervisorJob() + Dispatchers.Default)
        private var collecting = false
    }
}
