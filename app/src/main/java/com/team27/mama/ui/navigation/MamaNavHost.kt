package com.team27.mama.ui.navigation

import androidx.compose.runtime.Composable
import androidx.navigation.NavHostController
import androidx.navigation.compose.NavHost
import androidx.navigation.compose.composable
import androidx.navigation.compose.rememberNavController
import com.team27.mama.ui.screens.SetupScreen
import com.team27.mama.ui.screens.TriggerConfirmationScreen

/** Every screen of the app. Add a value here, then a composable() entry below. */
enum class Route(val path: String) {
    Setup("setup"),
    TriggerConfirmation("trigger_confirmation"),
}

/** The navigation shell. MainActivity shows this inside MamaTheme. */
@Composable
fun MamaNavHost(
    navController: NavHostController = rememberNavController(),
    startRoute: Route = Route.Setup,
) {
    NavHost(navController = navController, startDestination = startRoute.path) {
        composable(Route.Setup.path) {
            SetupScreen(
                // launchSingleTop: a double tap must not push the screen twice.
                onContinue = {
                    navController.navigate(Route.TriggerConfirmation.path) { launchSingleTop = true }
                },
            )
        }
        composable(Route.TriggerConfirmation.path) {
            TriggerConfirmationScreen(
                // Guard: a double tap must not pop the start screen and leave a blank host.
                onBack = {
                    if (navController.previousBackStackEntry != null) navController.popBackStack()
                },
            )
        }
    }
}
