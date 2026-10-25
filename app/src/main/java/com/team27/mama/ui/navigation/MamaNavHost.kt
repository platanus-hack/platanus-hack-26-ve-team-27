package com.team27.mama.ui.navigation

import android.content.pm.PackageManager
import android.os.Build
import androidx.activity.compose.rememberLauncherForActivityResult
import androidx.activity.result.contract.ActivityResultContracts
import android.os.SystemClock
import androidx.compose.runtime.Composable
import androidx.compose.runtime.LaunchedEffect
import androidx.compose.runtime.rememberUpdatedState
import androidx.compose.runtime.collectAsState
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.remember
import androidx.compose.runtime.setValue
import androidx.compose.ui.platform.LocalContext
import androidx.core.content.ContextCompat
import kotlinx.coroutines.delay
import androidx.navigation.NavHostController
import androidx.navigation.compose.NavHost
import androidx.navigation.compose.composable
import androidx.navigation.compose.rememberNavController
import com.team27.mama.emergency.EmergencyControls
import com.team27.mama.emergency.toStatus
import com.team27.mama.relay.MeshPermissions
import com.team27.mama.relay.RelayControls
import com.team27.mama.relay.relayPermissionMissing
import com.team27.mama.contract.Trigger
import com.team27.mama.trigger.TriggerService
import com.team27.mama.ui.home.HomeModel
import com.team27.mama.ui.home.HomePhase
import com.team27.mama.ui.screens.HomeScreen
import com.team27.mama.ui.screens.ProfileScreen
import com.team27.mama.ui.screens.SettingsScreen
import com.team27.mama.ui.screens.SetupScreen
import com.team27.mama.ui.screens.TriggerConfirmationScreen

/** Every screen of the app. Add a value here, then a composable() entry below. */
enum class Route(val path: String) {
    Home("home"),
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
        composable(Route.Home.path) {
            HomeDestination(
                emergency = emergency,
                onOpenProfile = { navController.navigate(Route.Profile.path) { launchSingleTop = true } },
                onOpenSettings = relay?.let {
                    { navController.navigate(Route.Settings.path) { launchSingleTop = true } }
                },
                onEditData = { navController.navigate(Route.Setup.path) { launchSingleTop = true } },
            )
        }
        composable(Route.Setup.path) {
            // First-run setup (#15). A finished setup opens on its "done" state, which goes on to Home.
            // Opened from Home to change the data, it goes back to that Home.
            SetupScreen(
                onDone = {
                    if (navController.previousBackStackEntry != null) {
                        navController.popBackStack()
                        return@SetupScreen
                    }
                    navController.navigate(Route.Home.path) {
                        popUpTo(Route.Setup.path) { inclusive = true }
                        launchSingleTop = true
                    }
                },
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
            val context = LocalContext.current
            TriggerConfirmationScreen(
                onBack = {
                    // "Fue sin querer": stop the location service and its uploads, then leave this screen.
                    emergency?.cancel()
                    TriggerService.cancel(context)
                    if (navController.previousBackStackEntry != null) {
                        navController.popBackStack()
                    } else {
                        // Opened by the trigger as the start screen: nothing below it to pop, so go home.
                        navController.navigate(Route.Home.path) {
                            popUpTo(Route.TriggerConfirmation.path) { inclusive = true }
                            launchSingleTop = true
                        }
                    }
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

/**
 * Home's wiring: a [HomeModel] driven by the clock and by [TriggerService.progress], with the
 * trigger and the cancel it asks for. The model is rebuilt when the screen is recreated; it then
 * adopts the incident the service is still running.
 */
@Composable
private fun HomeDestination(
    emergency: EmergencyControls?,
    onOpenProfile: () -> Unit,
    onOpenSettings: (() -> Unit)?,
    onEditData: () -> Unit,
) {
    val context = LocalContext.current
    val appContext = context.applicationContext
    val model = remember { HomeModel { TriggerService.start(appContext, Trigger.IN_APP) } }
    var state by remember { mutableStateOf(model.state) }
    val progress by TriggerService.progress.collectAsState()
    val latestProgress by rememberUpdatedState(progress)
    LaunchedEffect(model) {
        while (true) {
            model.update(SystemClock.elapsedRealtime(), latestProgress)
            state = model.state
            // A frame while the ring fills, a few times a second otherwise.
            delay(if (model.state.phase == HomePhase.Arming) 16L else 100L)
        }
    }
    HomeScreen(
        state = state,
        onPressStart = {
            model.pressStart(SystemClock.elapsedRealtime())
            state = model.state
        },
        onPressEnd = {
            model.pressEnd()
            state = model.state
        },
        onActivate = {
            model.activate(SystemClock.elapsedRealtime())
            state = model.state
        },
        onCancel = {
            // "Fue sin querer": stop the offline emergency and the location service with its uploads.
            model.cancel(SystemClock.elapsedRealtime())
            state = model.state
            emergency?.cancel()
            TriggerService.cancel(context)
        },
        onOpenProfile = onOpenProfile,
        onOpenSettings = onOpenSettings,
        onEditData = onEditData,
    )
}

private fun isGranted(context: android.content.Context, permission: String): Boolean =
    ContextCompat.checkSelfPermission(context, permission) == PackageManager.PERMISSION_GRANTED
