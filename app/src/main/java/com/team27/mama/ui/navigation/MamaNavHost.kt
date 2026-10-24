package com.team27.mama.ui.navigation

import androidx.compose.runtime.Composable
import androidx.compose.runtime.collectAsState
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
            )
        }
    }
}
