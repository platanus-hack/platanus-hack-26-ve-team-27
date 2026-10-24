package com.team27.mama.ui.navigation

import androidx.compose.runtime.Composable
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.remember
import androidx.compose.runtime.rememberCoroutineScope
import androidx.navigation.NavHostController
import androidx.navigation.compose.NavHost
import androidx.navigation.compose.composable
import androidx.navigation.compose.rememberNavController
import com.team27.mama.call.CallSession
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
 * call is rejected or hung up.
 */
@Composable
fun MamaNavHost(
    navController: NavHostController = rememberNavController(),
    startRoute: Route = Route.Setup,
    newCallSession: (CoroutineScope) -> CallSession = { FakeCallSession(it) },
    onCancelIncident: () -> Unit = {},
) {
    // One session per ringing call, shared by the incoming and in-call screens.
    val callScope = rememberCoroutineScope()
    val call = remember { mutableStateOf<CallSession?>(null) }
    // Back to where the call came from; started on the call itself, back to the start screen.
    val leaveCall: () -> Unit = {
        call.value = null
        if (navController.previousBackStackEntry != null) {
            navController.popBackStack()
        } else {
            navController.navigate(Route.Setup.path) { popUpTo(0) }
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
            val session = remember(entry) { newCallSession(callScope).also { call.value = it } }
            IncomingCallScreen(
                session = session,
                onAnswer = {
                    // The ringing screen leaves the back stack: back from the call never rings again.
                    navController.navigate(Route.InCall.path) {
                        popUpTo(Route.IncomingCall.path) { inclusive = true }
                        launchSingleTop = true
                    }
                },
                onCancel = {
                    onCancelIncident()
                    leaveCall()
                },
            )
        }
        composable(Route.InCall.path) { entry ->
            // Opened straight on the call (no ringing first): start one already answered.
            val session = remember(entry) {
                call.value ?: newCallSession(callScope).also {
                    it.answer()
                    call.value = it
                }
            }
            InCallScreen(
                session = session,
                onCancel = {
                    onCancelIncident()
                    leaveCall()
                },
                onEnded = leaveCall,
            )
        }
    }
}
