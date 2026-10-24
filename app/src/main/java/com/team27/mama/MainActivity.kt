package com.team27.mama

import android.os.Bundle
import androidx.activity.ComponentActivity
import androidx.activity.compose.setContent
import androidx.activity.enableEdgeToEdge
import com.team27.mama.ui.navigation.MamaNavHost
import com.team27.mama.ui.theme.MamaTheme

class MainActivity : ComponentActivity() {
    override fun onCreate(savedInstanceState: Bundle?) {
        super.onCreate(savedInstanceState)
        enableEdgeToEdge()
        setContent {
            MamaTheme {
                val graph = (application as MamaApp).graph
                MamaNavHost(emergency = graph.emergency, relay = graph.relayControls)
            }
        }
    }
}
