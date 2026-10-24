package com.team27.mama.ui.screens

import android.app.Activity
import android.content.res.Configuration
import androidx.compose.foundation.BorderStroke
import androidx.compose.foundation.border
import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Box
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.WindowInsets
import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.layout.safeDrawing
import androidx.compose.foundation.layout.size
import androidx.compose.foundation.layout.windowInsetsPadding
import androidx.compose.foundation.shape.CircleShape
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.Surface
import androidx.compose.material3.Text
import androidx.compose.runtime.Composable
import androidx.compose.runtime.DisposableEffect
import androidx.compose.runtime.LaunchedEffect
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.remember
import androidx.compose.runtime.saveable.rememberSaveable
import androidx.compose.runtime.setValue
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.draw.clip
import androidx.compose.ui.platform.LocalView
import androidx.compose.ui.tooling.preview.Preview
import androidx.compose.ui.unit.dp
import androidx.core.view.WindowCompat
import androidx.lifecycle.compose.collectAsStateWithLifecycle
import com.team27.mama.R
import com.team27.mama.call.CallPhase
import com.team27.mama.call.CallSession
import com.team27.mama.call.CallState
import com.team27.mama.call.Caption
import com.team27.mama.call.Speaker
import com.team27.mama.ui.components.AudioWave
import com.team27.mama.ui.components.CallButton
import com.team27.mama.ui.components.CallToggleButton
import com.team27.mama.ui.components.CaptionsPanel
import com.team27.mama.ui.components.MamaCharacter
import com.team27.mama.ui.components.MamaPose
import com.team27.mama.ui.theme.MamaSpacing
import com.team27.mama.ui.theme.MamaTheme
import com.team27.mama.ui.theme.hangUp
import com.team27.mama.ui.theme.onHangUp
import kotlinx.coroutines.flow.MutableStateFlow
import kotlinx.coroutines.flow.StateFlow
import java.util.Locale
import kotlin.time.Duration
import kotlin.time.Duration.Companion.seconds

/*
 * The call with Mamá once answered (#94, design D). Always dark, like the
 * phone's own call screen, so it forces the dark scheme even in light mode.
 * Bystander rule: the captions are an ordinary call accessory. No verdict, no
 * notice to the family, no highlighted phrase. Colgar cancels the incident
 * (it replaces "Fue sin querer" on this path) without saying so.
 * Silenciar and Altavoz are screen-only toggles until #97 wires the audio.
 */

@Composable
fun InCallScreen(session: CallSession, onCancel: () -> Unit, onEnded: () -> Unit = {}) {
    val state by session.state.collectAsStateWithLifecycle()
    var hungUp by remember { mutableStateOf(false) }
    // The other side ended the call (the real engine, #97): leave without cancelling.
    LaunchedEffect(state.phase) {
        if (state.phase == CallPhase.Ended && !hungUp) onEnded()
    }
    DarkSystemBars()
    MamaTheme(darkTheme = true) {
        InCallContent(
            state = state,
            onHangUp = {
                hungUp = true
                session.hangUp()
                onCancel()
            },
        )
    }
}

@Composable
private fun InCallContent(state: CallState, onHangUp: () -> Unit) {
    var muted by rememberSaveable { mutableStateOf(false) }
    var speaker by rememberSaveable { mutableStateOf(true) }
    Surface(modifier = Modifier.fillMaxSize(), color = MaterialTheme.colorScheme.background) {
        Column(
            modifier = Modifier
                .fillMaxSize()
                .windowInsetsPadding(WindowInsets.safeDrawing)
                .padding(horizontal = MamaSpacing.screen, vertical = MamaSpacing.l),
            horizontalAlignment = Alignment.CenterHorizontally,
            verticalArrangement = Arrangement.spacedBy(MamaSpacing.m),
        ) {
            Column(horizontalAlignment = Alignment.CenterHorizontally) {
                Text(
                    text = "Mamá",
                    style = MaterialTheme.typography.displaySmall,
                    color = MaterialTheme.colorScheme.onBackground,
                )
                Text(
                    text = formatElapsed(state.elapsed),
                    style = MaterialTheme.typography.titleMedium,
                    color = MaterialTheme.colorScheme.onSurfaceVariant,
                )
            }
            Box(
                modifier = Modifier
                    .size(168.dp)
                    .border(BorderStroke(2.dp, MaterialTheme.colorScheme.outlineVariant), CircleShape),
                contentAlignment = Alignment.Center,
            ) {
                MamaCharacter(pose = MamaPose.Listening, size = 148.dp, modifier = Modifier.clip(CircleShape))
            }
            AudioWave(speaking = state.speaking)
            CaptionsPanel(
                captions = state.captions,
                modifier = Modifier
                    .fillMaxWidth()
                    .weight(1f),
            )
            Row(
                modifier = Modifier.fillMaxWidth(),
                horizontalArrangement = Arrangement.SpaceEvenly,
                verticalAlignment = Alignment.Bottom,
            ) {
                CallToggleButton(
                    label = "Silenciar",
                    icon = if (muted) R.drawable.ic_mic_off else R.drawable.ic_mic,
                    checked = muted,
                    onCheckedChange = { muted = it },
                )
                CallButton(
                    label = "Colgar",
                    icon = R.drawable.ic_call_end,
                    containerColor = MaterialTheme.colorScheme.hangUp,
                    contentColor = MaterialTheme.colorScheme.onHangUp,
                    onClick = onHangUp,
                )
                CallToggleButton(
                    label = "Altavoz",
                    icon = R.drawable.ic_volume_up,
                    checked = speaker,
                    onCheckedChange = { speaker = it },
                )
            }
        }
    }
}

/** Light status and navigation bar icons while this dark screen shows, restored on leave. */
@Composable
private fun DarkSystemBars() {
    val view = LocalView.current
    if (view.isInEditMode) return
    DisposableEffect(view) {
        val window = (view.context as? Activity)?.window ?: return@DisposableEffect onDispose {}
        val bars = WindowCompat.getInsetsController(window, view)
        val lightStatus = bars.isAppearanceLightStatusBars
        val lightNavigation = bars.isAppearanceLightNavigationBars
        bars.isAppearanceLightStatusBars = false
        bars.isAppearanceLightNavigationBars = false
        onDispose {
            bars.isAppearanceLightStatusBars = lightStatus
            bars.isAppearanceLightNavigationBars = lightNavigation
        }
    }
}

/** 42 s -> "00:42"; past an hour the minutes keep counting. */
internal fun formatElapsed(elapsed: Duration): String {
    val total = elapsed.inWholeSeconds.coerceAtLeast(0)
    return String.format(Locale.ROOT, "%02d:%02d", total / 60, total % 60)
}

// The render's moment: four lines said, Mamá halfway through the fifth.
private val previewSession = object : CallSession {
    override val state: StateFlow<CallState> = MutableStateFlow(
        CallState(
            phase = CallPhase.Connected,
            elapsed = 42.seconds,
            speaking = Speaker.Mama,
            captions = listOf(
                Caption(Speaker.Mama, "¡Hola, mi amor! ¿Cómo vas? ¿Ya saliste del trabajo?", partial = false),
                Caption(Speaker.You, "Sí, mami, ya voy saliendo.", partial = false),
                Caption(Speaker.Mama, "¿Y a qué hora llegas? Te guardé comida.", partial = false),
                Caption(Speaker.You, "Dile a la tía Carmen que llego tarde.", partial = false),
                Caption(Speaker.Mama, "Ay, bueno, tranquilo, yo le digo. ¿Vienes en el carro", partial = true),
            ),
        ),
    )

    override fun answer() {}

    override fun hangUp() {}
}

// Both previews must come out dark: the screen forces the dark scheme.
@Preview(showBackground = true, heightDp = 800)
@Composable
private fun InCallScreenPreview() {
    MamaTheme { InCallScreen(previewSession, onCancel = {}) }
}

@Preview(showBackground = true, heightDp = 800, uiMode = Configuration.UI_MODE_NIGHT_YES)
@Composable
private fun InCallScreenDarkPreview() {
    MamaTheme { InCallScreen(previewSession, onCancel = {}) }
}
