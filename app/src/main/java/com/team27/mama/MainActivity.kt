package com.team27.mama

import android.os.Bundle
import androidx.activity.ComponentActivity
import androidx.activity.compose.setContent
import androidx.activity.enableEdgeToEdge
import androidx.navigation.compose.rememberNavController
import com.team27.mama.incomingcall.IncomingCallWindow
import com.team27.mama.ui.navigation.MamaNavHost
import com.team27.mama.ui.navigation.Route
import com.team27.mama.ui.theme.MamaTheme

class MainActivity : ComponentActivity() {
    override fun onCreate(savedInstanceState: Bundle?) {
        super.onCreate(savedInstanceState)
        enableEdgeToEdge()
        // "route" extra opens a screen directly, e.g. adb shell am start -n com.team27.mama/.MainActivity --es route incoming_call
        val start = Route.entries.firstOrNull { it.path == intent.getStringExtra("route") } ?: Route.Setup
        // Mamá's call notification (#96): over the lock screen while a call screen is up.
        val call = IncomingCallWindow.open(this, intent)
        setContent {
            MamaTheme {
                val navController = rememberNavController()
                call?.Follow(navController)
                MamaNavHost(navController = navController, startRoute = start)
            }
        }
    }
}
