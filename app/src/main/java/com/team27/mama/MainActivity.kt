package com.team27.mama

import android.os.Bundle
import androidx.activity.ComponentActivity
import androidx.activity.compose.setContent
import androidx.activity.enableEdgeToEdge
import com.team27.mama.ui.navigation.MamaNavHost
import com.team27.mama.ui.theme.MamaTheme
import kotlinx.coroutines.Dispatchers
import kotlinx.coroutines.withContext

class MainActivity : ComponentActivity() {
    override fun onCreate(savedInstanceState: Bundle?) {
        super.onCreate(savedInstanceState)
        enableEdgeToEdge()
        setContent {
            MamaTheme {
                MamaNavHost(
                    finishOnboarding = {
                        withContext(Dispatchers.IO) { (application as MamaApp).graph.onboardingGate.finish() }
                    },
                    emergency = (application as MamaApp).graph.emergency,
                )
            }
        }
    }
}
