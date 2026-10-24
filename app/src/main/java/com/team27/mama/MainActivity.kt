package com.team27.mama

import android.os.Bundle
import androidx.activity.ComponentActivity
import androidx.activity.compose.setContent
import androidx.activity.enableEdgeToEdge
import com.team27.mama.ui.navigation.MamaNavHost
import com.team27.mama.ui.navigation.Route
import com.team27.mama.ui.theme.MamaTheme

class MainActivity : ComponentActivity() {
    override fun onCreate(savedInstanceState: Bundle?) {
        super.onCreate(savedInstanceState)
        enableEdgeToEdge()
        // "route" extra opens a screen directly, e.g. adb shell am start -n com.team27.mama/.MainActivity --es route incoming_call
        val start = Route.entries.firstOrNull { it.path == intent.getStringExtra("route") } ?: Route.Setup
        setContent {
            MamaTheme {
                MamaNavHost(startRoute = start)
            }
        }
    }
}
