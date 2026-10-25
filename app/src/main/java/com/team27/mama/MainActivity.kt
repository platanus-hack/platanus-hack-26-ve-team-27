package com.team27.mama

import android.os.Bundle
import androidx.activity.ComponentActivity
import androidx.activity.compose.setContent
import androidx.activity.enableEdgeToEdge
import com.team27.mama.setup.SetupStore
import com.team27.mama.setup.SharedPreferencesStore
import com.team27.mama.ui.navigation.MamaNavHost
import com.team27.mama.ui.navigation.Route
import com.team27.mama.ui.theme.MamaTheme

class MainActivity : ComponentActivity() {
    override fun onCreate(savedInstanceState: Bundle?) {
        super.onCreate(savedInstanceState)
        enableEdgeToEdge()
        val graph = (application as MamaApp).graph
        // "route" extra opens a screen directly, e.g. adb shell am start -n com.team27.mama/.MainActivity --es route incoming_call
        // Settings is only registered with the relay (flag on); flag off it falls back to Home, or Setup before setup is done.
        val start = Route.entries.firstOrNull {
            it.path == intent.getStringExtra("route") && (it != Route.Settings || graph.relayControls != null)
        } ?: if (SetupStore(SharedPreferencesStore.forSetup(this)).setupCompleted) Route.Home else Route.Setup
        setContent {
            MamaTheme {
                MamaNavHost(startRoute = start, emergency = graph.emergency, relay = graph.relayControls)
            }
        }
    }
}
