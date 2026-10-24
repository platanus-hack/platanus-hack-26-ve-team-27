package com.team27.mama.ui.navigation

import android.content.pm.PackageManager
import android.os.Build
import androidx.activity.compose.rememberLauncherForActivityResult
import androidx.activity.result.contract.ActivityResultContracts
import androidx.compose.runtime.Composable
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
import com.team27.mama.ui.screens.ProfileScreen
import com.team27.mama.ui.screens.SettingsScreen
import com.team27.mama.ui.screens.SetupScreen
import com.team27.mama.ui.screens.TriggerConfirmationScreen

/** Every screen of the app. Add a value here, then a composable() entry below. */
enum class Route(val path: String) {
    Setup("setup"),
    TriggerConfirmation("trigger_confirmation"),
    Settings("settings"),
    Profile("profile"),
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
            // Only shows the offline emergency. The trigger service declares it when its alert
            // fails (emergency.OfflineFallback), and setup asks for the Bluetooth permissions.
            val state = emergency?.state?.collectAsState()?.value
            TriggerConfirmationScreen(
                // Guard: a double tap must not pop the start screen and leave a blank host.
                onBack = {
                    emergency?.cancel()
                    if (navController.previousBackStackEntry != null) navController.popBackStack()
                },
                status = state?.toStatus(),
                onOpenProfile = { navController.navigate(Route.Profile.path) { launchSingleTop = true } },
            )
        }
        composable(Route.Profile.path) {
            // Read-only profile (#85), opened from the trigger screen.
            ProfileScreen(
                onBack = {
                    if (navController.previousBackStackEntry != null) navController.popBackStack()
                },
            )
        }
    }
}

private fun isGranted(context: android.content.Context, permission: String): Boolean =
    ContextCompat.checkSelfPermission(context, permission) == PackageManager.PERMISSION_GRANTED
