package com.team27.mama.ui.navigation

import android.Manifest
import android.content.pm.PackageManager
import android.os.Build
import androidx.activity.compose.rememberLauncherForActivityResult
import androidx.activity.result.contract.ActivityResultContracts
import androidx.compose.runtime.Composable
import androidx.compose.runtime.LaunchedEffect
import androidx.compose.runtime.collectAsState
import androidx.compose.ui.platform.LocalContext
import androidx.core.content.ContextCompat
import androidx.navigation.NavHostController
import androidx.navigation.compose.NavHost
import androidx.navigation.compose.composable
import androidx.navigation.compose.rememberNavController
import com.team27.mama.emergency.EmergencyControls
import com.team27.mama.emergency.toStatus
import com.team27.mama.ui.screens.SetupScreen
import com.team27.mama.ui.screens.TriggerConfirmationScreen

/** Every screen of the app. Add a value here, then a composable() entry below. */
enum class Route(val path: String) {
    Setup("setup"),
    TriggerConfirmation("trigger_confirmation"),
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
) {
    NavHost(navController = navController, startDestination = startRoute.path) {
        composable(Route.Setup.path) {
            // First-run setup (#15). A finished setup opens on its "done" state.
            SetupScreen()
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
