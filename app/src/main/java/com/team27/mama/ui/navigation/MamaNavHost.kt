package com.team27.mama.ui.navigation

import android.Manifest
import android.content.pm.PackageManager
import android.os.Build
import androidx.activity.compose.rememberLauncherForActivityResult
import androidx.activity.result.contract.ActivityResultContracts
import androidx.compose.runtime.Composable
import androidx.compose.runtime.LaunchedEffect
import androidx.compose.runtime.collectAsState
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.remember
import androidx.compose.runtime.setValue
import androidx.compose.ui.platform.LocalContext
import androidx.core.content.ContextCompat
import androidx.navigation.NavHostController
import androidx.navigation.compose.NavHost
import androidx.navigation.compose.composable
import androidx.navigation.compose.rememberNavController
import com.team27.mama.emergency.EmergencyControls
import com.team27.mama.emergency.toStatus
import com.team27.mama.relay.MeshPermissions
import com.team27.mama.relay.RelayControls
import com.team27.mama.relay.relayPermissionMissing
import com.team27.mama.ui.screens.SettingsScreen
import com.team27.mama.ui.screens.SetupScreen
import com.team27.mama.ui.screens.TriggerConfirmationScreen

/** Every screen of the app. Add a value here, then a composable() entry below. */
enum class Route(val path: String) {
    Setup("setup"),
    TriggerConfirmation("trigger_confirmation"),
    Settings("settings"),
}

/**
 * The navigation shell. MainActivity shows this inside MamaTheme. [emergency] is null with the
 * offline emergency flag off: the trigger screen then keeps its placeholder steps.
 */
@Composable
fun MamaNavHost(
    navController: NavHostController = rememberNavController(),
    startRoute: Route = Route.Setup,
    emergency: EmergencyControls? = null,
    relay: RelayControls? = null,
) {
    NavHost(navController = navController, startDestination = startRoute.path) {
        composable(Route.Setup.path) {
            // First-run setup (#15). A finished setup opens on its "done" state.
            SetupScreen(
                onOpenSettings = relay?.let {
                    { navController.navigate(Route.Settings.path) { launchSingleTop = true } }
                },
            )
        }
        if (relay != null) {
            composable(Route.Settings.path) {
                val context = LocalContext.current
                val enabled by relay.enabled.collectAsState()
                val count by relay.relayedCount.collectAsState()
                var refused by remember { mutableStateOf(false) }
                val sdk = Build.VERSION.SDK_INT
                val launcher = rememberLauncherForActivityResult(ActivityResultContracts.RequestMultiplePermissions()) { result ->
                    if (MeshPermissions.satisfied(sdk) { result[it] == true || isGranted(context, it) }) {
                        refused = false
                        relay.setEnabled(true)
                    } else {
                        refused = true
                    }
                }
                SettingsScreen(
                    enabled = enabled,
                    relayedCount = count,
                    // Re-checked on each composition: a permission revoked in system settings shows the card when the screen reopens.
                    permissionMissing = relayPermissionMissing(
                        enabled = enabled,
                        satisfied = MeshPermissions.satisfied(sdk) { isGranted(context, it) },
                        refused = refused,
                    ),
                    onEnabledChange = { on ->
                        if (!on) {
                            refused = false
                            relay.setEnabled(false)
                        } else if (MeshPermissions.satisfied(sdk) { isGranted(context, it) }) {
                            refused = false
                            relay.setEnabled(true)
                        } else {
                            val missing = MeshPermissions.requested(sdk).filter { !isGranted(context, it) }
                            launcher.launch(missing.toTypedArray())
                        }
                    },
                    // Guard: a double tap must not pop the start screen and leave a blank host.
                    onBack = { if (navController.previousBackStackEntry != null) navController.popBackStack() },
                )
            }
        }
        composable(Route.TriggerConfirmation.path) {
            val context = LocalContext.current
            val state = emergency?.state?.collectAsState()?.value
            val launcher = rememberLauncherForActivityResult(ActivityResultContracts.RequestMultiplePermissions()) {
                // HTTP and SMS need neither BLE nor location, so declare whatever the user answered.
                emergency?.declare()
            }
            LaunchedEffect(emergency) {
                // Flag off (no emergency): the placeholder steps, no permission prompt, no mesh.
                if (emergency == null) return@LaunchedEffect
                val missing = emergencyPermissions().filter {
                    ContextCompat.checkSelfPermission(context, it) != PackageManager.PERMISSION_GRANTED
                }
                if (missing.isEmpty()) emergency.declare() else launcher.launch(missing.toTypedArray())
            }
            TriggerConfirmationScreen(
                // Guard: a double tap must not pop the start screen and leave a blank host.
                onBack = {
                    emergency?.cancel()
                    if (navController.previousBackStackEntry != null) navController.popBackStack()
                },
                status = state?.toStatus(),
            )
        }
    }
}

/**
 * Runtime permissions for the mesh and the location. Setup (#15) already asks for location, so
 * usually only the Bluetooth ones are still missing; only missing ones are requested.
 * Android 12+ ignores a FINE-only request.
 */
private fun emergencyPermissions(): List<String> {
    val location = listOf(Manifest.permission.ACCESS_FINE_LOCATION, Manifest.permission.ACCESS_COARSE_LOCATION)
    return if (Build.VERSION.SDK_INT >= Build.VERSION_CODES.S) {
        listOf(
            Manifest.permission.BLUETOOTH_SCAN,
            Manifest.permission.BLUETOOTH_ADVERTISE,
            Manifest.permission.BLUETOOTH_CONNECT,
        ) + location
    } else {
        location
    }
}

private fun isGranted(context: android.content.Context, permission: String): Boolean =
    ContextCompat.checkSelfPermission(context, permission) == PackageManager.PERMISSION_GRANTED
