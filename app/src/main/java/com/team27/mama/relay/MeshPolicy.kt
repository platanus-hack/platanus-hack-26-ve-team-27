package com.team27.mama.relay

import android.Manifest
import com.team27.mama.emergency.EmergencyPhase
import kotlinx.coroutines.CancellationException
import kotlinx.coroutines.CoroutineScope
import kotlinx.coroutines.flow.Flow
import kotlinx.coroutines.flow.combine
import kotlinx.coroutines.launch

enum class MeshMode { OFF, APP_ONLY, SERVICE }

fun meshMode(appVisible: Boolean, relayEnabled: Boolean, emergencyActive: Boolean): MeshMode = when {
    relayEnabled || emergencyActive -> MeshMode.SERVICE
    appVisible -> MeshMode.APP_ONLY
    else -> MeshMode.OFF
}

/** True while an emergency needs the mesh: everything but IDLE and CANCELLED. */
fun EmergencyPhase.keepsMeshOn(): Boolean = this != EmergencyPhase.IDLE && this != EmergencyPhase.CANCELLED

/**
 * Applies [meshMode] on every emission of any input (no distinctUntilChanged), so a service start
 * refused in the background is retried on the next app open. The service call comes first.
 */
class MeshController(
    private val appVisible: Flow<Boolean>,
    private val relayEnabled: Flow<Boolean>,
    private val emergencyActive: Flow<Boolean>,
    private val startGateway: () -> Unit,
    private val stopGateway: () -> Unit,
    private val startService: () -> Unit,
    private val stopService: () -> Unit,
    private val scope: CoroutineScope,
    private val onError: (String) -> Unit = {},
) {
    fun start() {
        scope.launch {
            combine(appVisible, relayEnabled, emergencyActive, ::meshMode).collect { mode ->
                when (mode) {
                    MeshMode.OFF -> {
                        guarded("stopService", stopService)
                        guarded("stopGateway", stopGateway)
                    }
                    MeshMode.APP_ONLY -> {
                        guarded("stopService", stopService)
                        guarded("startGateway", startGateway)
                    }
                    MeshMode.SERVICE -> {
                        guarded("startService", startService)
                        guarded("startGateway", startGateway)
                    }
                }
            }
        }
    }

    /** One failing call (a refused service start, a BLE SecurityException) must not end the collector. */
    private fun guarded(tag: String, call: () -> Unit) {
        try {
            call()
        } catch (e: Exception) {
            if (e is CancellationException) throw e
            onError(tag)
        }
    }
}

object MeshPermissions {
    private const val API_S = 31
    private const val API_TIRAMISU = 33

    private val BLE = listOf(
        Manifest.permission.BLUETOOTH_SCAN,
        Manifest.permission.BLUETOOTH_ADVERTISE,
        Manifest.permission.BLUETOOTH_CONNECT,
    )
    private val LOCATION = listOf(
        Manifest.permission.ACCESS_FINE_LOCATION,
        Manifest.permission.ACCESS_COARSE_LOCATION,
    )

    fun required(sdk: Int): List<String> = if (sdk >= API_S) BLE + LOCATION else LOCATION

    /** [required] plus the notification permission on API 33+ (asked for, not needed to enable the relay). */
    fun requested(sdk: Int): List<String> =
        if (sdk >= API_TIRAMISU) required(sdk) + Manifest.permission.POST_NOTIFICATIONS else required(sdk)

    /** Every BLE permission (API 31+) is granted and at least one of fine or coarse location is. */
    fun satisfied(sdk: Int, granted: (String) -> Boolean): Boolean {
        val ble = sdk < API_S || BLE.all(granted)
        return ble && LOCATION.any(granted)
    }
}
