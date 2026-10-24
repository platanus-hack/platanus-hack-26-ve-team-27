package com.team27.mama.ui.navigation

import androidx.compose.runtime.Composable
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.remember
import androidx.compose.runtime.rememberCoroutineScope
import androidx.compose.runtime.setValue
import androidx.navigation.NavHostController
import androidx.navigation.compose.NavHost
import androidx.navigation.compose.composable
import androidx.navigation.compose.rememberNavController
import com.team27.mama.onboarding.OnboardingResult
import com.team27.mama.onboarding.userMessage
import com.team27.mama.ui.screens.SetupScreen
import com.team27.mama.ui.screens.TriggerConfirmationScreen
import kotlinx.coroutines.launch

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
    finishOnboarding: suspend () -> OnboardingResult = { OnboardingResult.Done },
) {
    var busy by remember { mutableStateOf(false) }
    var errorMessage by remember { mutableStateOf<String?>(null) }
    val scope = rememberCoroutineScope()
    NavHost(navController = navController, startDestination = startRoute.path) {
        composable(Route.Setup.path) {
            SetupScreen(
                // launchSingleTop: a double tap must not push the screen twice.
                onContinue = {
                    if (!busy) {
                        scope.launch {
                            errorMessage = null
                            busy = true
                            val result = finishOnboarding()
                            busy = false
                            if (result == OnboardingResult.Done) {
                                navController.navigate(Route.TriggerConfirmation.path) { launchSingleTop = true }
                            } else {
                                errorMessage = result.userMessage()
                            }
                        }
                    }
                },
                busy = busy,
                errorMessage = errorMessage,
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
