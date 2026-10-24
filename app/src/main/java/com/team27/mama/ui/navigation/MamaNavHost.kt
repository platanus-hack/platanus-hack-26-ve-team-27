package com.team27.mama.ui.navigation

import androidx.activity.compose.BackHandler
import androidx.compose.runtime.Composable
import androidx.compose.runtime.remember
import androidx.lifecycle.viewmodel.compose.viewModel
import androidx.navigation.NavHostController
import androidx.navigation.compose.NavHost
import androidx.navigation.compose.composable
import androidx.navigation.compose.rememberNavController
import com.team27.mama.call.CallPhase
import com.team27.mama.call.CallSession
import com.team27.mama.call.CallViewModel
import com.team27.mama.call.FakeCallSession
import com.team27.mama.ui.screens.InCallScreen
import com.team27.mama.ui.screens.IncomingCallScreen
import com.team27.mama.ui.screens.SetupScreen
import com.team27.mama.ui.screens.TriggerConfirmationScreen
import kotlinx.coroutines.CoroutineScope

/** Every screen of the app. Add a value here, then a composable() entry below. */
enum class Route(val path: String) {
    Setup("setup"),
    TriggerConfirmation("trigger_confirmation"),
    IncomingCall("incoming_call"),
    InCall("in_call"),
}

/**
 * The navigation shell. MainActivity shows this inside MamaTheme.
 * [newCallSession] builds the engine behind the in-app call (#94): the demo
 * script for now, the real agent with #97. [onCancelIncident] runs when the
 * call is rejected or hung up; #96/#97 wire it to the incident.
 */
@Composable
fun MamaNavHost(
    navController: NavHostController = rememberNavController(),
    startRoute: Route = Route.Setup,
    newCallSession: (CoroutineScope) -> CallSession = { FakeCallSession(it) },
    onCancelIncident: () -> Unit = {},
) {
    // Activity-scoped, so a rotation does not restart the call.
    val calls: CallViewModel = viewModel()
    // Both run only while a call screen is on top, so a double tap, or a hang-up
    // that also ends the session, cannot cancel twice or pop the screen underneath.
    val onCallScreen = {
        navController.currentDestination?.route.let { it == Route.IncomingCall.path || it == Route.InCall.path }
    }
    // Back to where the call came from; started on the call itself, back to the start screen.
    val leaveCall: () -> Unit = {
        calls.end()
        if (onCallScreen()) {
            if (navController.previousBackStackEntry != null) {
                navController.popBackStack()
            } else {
                navController.navigate(Route.Setup.path) { popUpTo(navController.graph.id) }
            }
        }
    }
    val cancelCall: () -> Unit = {
        if (onCallScreen()) {
            onCancelIncident()
            leaveCall()
        }
    }

    NavHost(navController = navController, startDestination = startRoute.path) {
        composable(Route.Setup.path) {
            // First-run setup (#15). A finished setup opens on its "done" state.
            SetupScreen()
        }
        composable(Route.TriggerConfirmation.path) {
            TriggerConfirmationScreen(
                // Guard: a double tap must not pop the start screen and leave a blank host.
                onBack = {
                    if (navController.previousBackStackEntry != null) navController.popBackStack()
                },
            )
        }
        composable(Route.IncomingCall.path) { entry ->
            val session = remember(entry) {
                calls.current()?.takeIf { it.state.value.phase == CallPhase.Ringing } ?: calls.start(newCallSession)
            }
            // System back ends the call but never cancels the incident: only Rechazar does.
            BackHandler(onBack = leaveCall)
            IncomingCallScreen(
                session = session,
                onAnswer = {
                    // The ringing screen leaves the back stack: back from the call never rings again.
                    navController.navigate(Route.InCall.path) {
                        popUpTo(Route.IncomingCall.path) { inclusive = true }
                        launchSingleTop = true
                    }
                },
                onCancel = cancelCall,
            )
        }
        composable(Route.InCall.path) { entry ->
            // Opened straight on the call (no ringing first): start one already answered.
            val session = remember(entry) { calls.current() ?: calls.start(newCallSession).also { it.answer() } }
            BackHandler(onBack = leaveCall)
            InCallScreen(session = session, onCancel = cancelCall, onEnded = leaveCall)
        }
    }
}
