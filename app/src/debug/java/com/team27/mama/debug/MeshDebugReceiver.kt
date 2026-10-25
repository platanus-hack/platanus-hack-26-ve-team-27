package com.team27.mama.debug

import android.content.BroadcastReceiver
import android.content.Context
import android.content.Intent
import android.util.Log
import com.team27.mama.MamaApp
import com.team27.mama.emergency.ChannelOverride
import com.team27.mama.emergency.EmergencyOrchestrator
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
 *
 * Mesh actions (#56): MESH_START, MESH_SEND (string extra `text`), MESH_STOP.
 *
 * Emergency actions (#59): EMERGENCY_DECLARE and EMERGENCY_CANCEL call
 * `graph.emergency.declare()` / `cancel()`; `adb logcat -s MeshDebug MamaEmergency` shows the
 * result and the phase (never the text, the location or a number). Procedure:
 * 1. Build with the offline emergency flag, `MAMA_OFFLINE_EMERGENCY=1 ./gradlew assembleDebug`
 *    (without it `graph.emergency` is null and the hook only logs that the flag is off), and
 *    install the APK.
 * 2. Grant the runtime permissions the trigger screen would ask for, on each phone:
 *    `adb shell pm grant com.team27.mama android.permission.<name>` for BLUETOOTH_SCAN,
 *    BLUETOOTH_ADVERTISE, BLUETOOTH_CONNECT, ACCESS_FINE_LOCATION and ACCESS_COARSE_LOCATION.
 * 3. Send EMERGENCY_DECLARE, then EMERGENCY_CANCEL when done.
 *
 * Demo action (#61): CHANNEL_OVERRIDE with string extra `mode` = AUTO, NO_INTERNET, SMS_ONLY or
 * BLE_ONLY forces the channels of the next emergency (it is read once, at declare). Anything but
 * AUTO also skips the trigger's online AlertEvent, so the button goes straight to SMS and/or mesh
 * even with internet. It lives in memory: an app restart goes back to AUTO.
 *
 * Do not send MESH_STOP during an emergency test: it stops the same gateway singleton the
 * orchestrator broadcasts through.
 */
class MeshDebugReceiver : BroadcastReceiver() {

    override fun onReceive(context: Context, intent: Intent) {
        try {
            // Lazy: the emergency actions must not touch the gateway themselves.
            val gateway by lazy { MeshGateway.getInstance(context) }
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
                ACTION_CHANNEL_OVERRIDE -> {
                    val graph = (context.applicationContext as MamaApp).graph
                    val mode = intent.getStringExtra("mode")
                    val override = ChannelOverride.entries.firstOrNull { it.name.equals(mode, ignoreCase = true) }
                    if (override == null) {
                        Log.w(TAG, "override needs string extra 'mode': ${ChannelOverride.entries.joinToString()}")
                    } else {
                        graph.channelOverride.value = override
                        Log.i(TAG, "override=$override emergency=${graph.emergency != null}")
                    }
                }
                ACTION_EMERGENCY_DECLARE -> withEmergency(context) {
                    Log.i(TAG, "declare=${it.declare()} override=${graph(context)?.channelOverride?.value} phase=${it.state.value.phase}")
                }
                ACTION_EMERGENCY_CANCEL -> withEmergency(context) {
                    it.cancel()
                    Log.i(TAG, "cancel phase=${it.state.value.phase}")
                }
                else -> Log.w(TAG, "unknown action ${intent.action}")
            }
        } catch (e: Exception) {
            // Never crash the app from an adb hook (bad text, key store or Bluetooth errors).
            Log.w(TAG, "failed: ${e.javaClass.simpleName}: ${e.message}")
        }
    }

    /** Only the result and the phase are logged: never the text, the location or a number. */
    private inline fun withEmergency(context: Context, block: (EmergencyOrchestrator) -> Unit) {
        val emergency = graph(context)?.emergency
        if (emergency == null) {
            Log.w(TAG, "no emergency: build with MAMA_OFFLINE_EMERGENCY=1 (flag off)")
        } else {
            block(emergency)
        }
    }

    private fun graph(context: Context) = (context.applicationContext as? MamaApp)?.graph

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
        const val ACTION_EMERGENCY_DECLARE = "com.team27.mama.debug.EMERGENCY_DECLARE"
        const val ACTION_EMERGENCY_CANCEL = "com.team27.mama.debug.EMERGENCY_CANCEL"
        const val ACTION_CHANNEL_OVERRIDE = "com.team27.mama.debug.CHANNEL_OVERRIDE"

        private val scope = CoroutineScope(SupervisorJob() + Dispatchers.Default)
        private var collecting = false
    }
}
