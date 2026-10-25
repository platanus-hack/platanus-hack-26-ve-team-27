package com.team27.mama.ui.screens

import android.content.res.Configuration
import androidx.compose.animation.core.animateFloatAsState
import androidx.compose.animation.core.tween
import androidx.compose.foundation.gestures.detectTapGestures
import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Box
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.WindowInsets
import androidx.compose.foundation.layout.WindowInsetsSides
import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.only
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.layout.safeDrawing
import androidx.compose.foundation.layout.size
import androidx.compose.foundation.layout.windowInsetsPadding
import androidx.compose.foundation.rememberScrollState
import androidx.compose.foundation.shape.CircleShape
import androidx.compose.foundation.verticalScroll
import androidx.compose.material3.Card
import androidx.compose.material3.CardDefaults
import androidx.compose.material3.CenterAlignedTopAppBar
import androidx.compose.material3.ExperimentalMaterial3Api
import androidx.compose.material3.LinearProgressIndicator
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.Scaffold
import androidx.compose.material3.Surface
import androidx.compose.material3.Text
import androidx.compose.material3.TextButton
import androidx.compose.runtime.Composable
import androidx.compose.runtime.getValue
import androidx.compose.runtime.rememberUpdatedState
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.draw.clip
import androidx.compose.ui.graphics.graphicsLayer
import androidx.compose.ui.input.pointer.pointerInput
import androidx.compose.ui.semantics.LiveRegionMode
import androidx.compose.ui.semantics.Role
import androidx.compose.ui.semantics.contentDescription
import androidx.compose.ui.semantics.liveRegion
import androidx.compose.ui.semantics.onLongClick
import androidx.compose.ui.semantics.role
import androidx.compose.ui.semantics.semantics
import androidx.compose.ui.text.style.TextAlign
import androidx.compose.ui.tooling.preview.Preview
import androidx.compose.ui.unit.dp
import com.team27.mama.trigger.TriggerProgress
import com.team27.mama.ui.components.BlinkingDot
import com.team27.mama.ui.components.BouncingDots
import com.team27.mama.ui.components.HomeHalo
import com.team27.mama.ui.components.HomeHaloSize
import com.team27.mama.ui.components.MamaCharacter
import com.team27.mama.ui.components.MamaPose
import com.team27.mama.ui.components.VoiceWaveform
import com.team27.mama.ui.components.rememberAnimationsEnabled
import com.team27.mama.ui.home.HomeFailure
import com.team27.mama.ui.home.HomeModel
import com.team27.mama.ui.home.HomePhase
import com.team27.mama.ui.home.HomeState
import com.team27.mama.ui.theme.MamaSpacing
import com.team27.mama.ui.theme.MamaTheme
import com.team27.mama.ui.theme.MamaWordmark

/*
 * The home screen: Mamá's portrait is the trigger. Hold it for 1.5 s while a ring fills, then the
 * screen shows the call being placed and Mamá listening, in place. The logic lives in
 * ui/home/HomeModel.kt; this file only draws a HomeState and reports gestures.
 *
 * A bystander may read it (app/UI_RULES.md): neutral words only, and the pose while the call is
 * placed is Calling, never Typing. "Fue sin querer" is always on screen while an incident shows.
 */

private val PortraitSize = 196.dp

@OptIn(ExperimentalMaterial3Api::class)
@Composable
fun HomeScreen(
    state: HomeState,
    onPressStart: () -> Unit,
    onPressEnd: () -> Unit,
    onActivate: () -> Unit,
    onCancel: () -> Unit,
    onOpenProfile: () -> Unit,
    onOpenSettings: (() -> Unit)? = null,
) {
    val incidentShown = state.phase == HomePhase.Processing || state.phase == HomePhase.Listening
    Scaffold(
        topBar = {
            CenterAlignedTopAppBar(
                title = { MamaWordmark(compact = true) },
                actions = {
                    if (onOpenSettings != null) TextButton(onClick = onOpenSettings) { Text("Ajustes") }
                    TextButton(onClick = onOpenProfile) { Text("Perfil") }
                },
            )
        },
        bottomBar = { if (incidentShown) CancelBar(onCancel) },
    ) { padding ->
        Column(
            modifier = Modifier
                .fillMaxSize()
                .padding(padding)
                .verticalScroll(rememberScrollState())
                .padding(horizontal = MamaSpacing.screen, vertical = MamaSpacing.xl),
            horizontalAlignment = Alignment.CenterHorizontally,
            verticalArrangement = Arrangement.spacedBy(MamaSpacing.m),
        ) {
            Portrait(state, onPressStart, onPressEnd, onActivate)
            if (state.phase == HomePhase.Listening) VoiceWaveform()
            Headline(state.phase)
            Text(
                text = bodyFor(state.phase),
                style = MaterialTheme.typography.bodyLarge,
                color = MaterialTheme.colorScheme.onSurfaceVariant,
                textAlign = TextAlign.Center,
            )
            when (state.phase) {
                HomePhase.Processing -> StepBars(state.progress)
                HomePhase.Listening -> ElapsedPill(HomeModel.formatElapsed(state.listeningSeconds))
                HomePhase.Failed -> state.failure?.let { FailureRow(it) }
                else -> Unit
            }
        }
    }
}

private fun bodyFor(phase: HomePhase) = when (phase) {
    HomePhase.Idle, HomePhase.Failed -> "Mantén presionada su foto para llamarla."
    HomePhase.Arming -> "Sigue presionando…"
    HomePhase.Processing -> "Un momento, ya casi."
    HomePhase.Listening -> "Habla normal, como siempre."
    HomePhase.Cancelled -> "Volviendo al inicio."
}

private fun poseFor(phase: HomePhase) = when (phase) {
    HomePhase.Idle, HomePhase.Arming, HomePhase.Failed -> MamaPose.Idle
    // Never Typing here: it reads as "she is texting someone" to a bystander.
    HomePhase.Processing -> MamaPose.Calling
    HomePhase.Listening -> MamaPose.Listening
    HomePhase.Cancelled -> MamaPose.Relieved
}

/** The halo, the portrait and the hold gesture. */
@Composable
private fun Portrait(
    state: HomeState,
    onPressStart: () -> Unit,
    onPressEnd: () -> Unit,
    onActivate: () -> Unit,
) {
    val animate = rememberAnimationsEnabled()
    val interactive = state.phase == HomePhase.Idle || state.phase == HomePhase.Arming
    val scale by animateFloatAsState(
        targetValue = if (state.phase == HomePhase.Arming) 0.94f else 1f,
        animationSpec = tween(if (animate) 200 else 0),
        label = "portrait-scale",
    )
    val start by rememberUpdatedState(onPressStart)
    val end by rememberUpdatedState(onPressEnd)
    Box(modifier = Modifier.size(HomeHaloSize), contentAlignment = Alignment.Center) {
        HomeHalo(phase = state.phase, armProgress = state.armProgress)
        Box(
            modifier = Modifier
                .size(PortraitSize)
                .graphicsLayer {
                    scaleX = scale
                    scaleY = scale
                }
                .clip(CircleShape)
                .then(
                    if (interactive) {
                        Modifier.pointerInput(Unit) {
                            detectTapGestures(onPress = {
                                start()
                                tryAwaitRelease()
                                end()
                            })
                        }
                    } else {
                        Modifier
                    },
                )
                .semantics {
                    role = Role.Button
                    contentDescription = "Foto de Mamá. Mantén presionada para llamarla."
                    // TalkBack cannot hold a touch for 1.5 s: the same request as an action.
                    if (interactive) {
                        onLongClick(label = "Llamar a Mamá") {
                            onActivate()
                            true
                        }
                    }
                },
        ) {
            MamaCharacter(pose = poseFor(state.phase), size = PortraitSize)
        }
    }
}

@Composable
private fun Headline(phase: HomePhase) {
    val text = when (phase) {
        HomePhase.Idle, HomePhase.Arming, HomePhase.Failed -> "Mamá está lista"
        HomePhase.Processing -> "Llamando a Mamá"
        HomePhase.Listening -> "Mamá está en línea"
        HomePhase.Cancelled -> "Listo, quedó cancelado"
    }
    Row(verticalAlignment = Alignment.CenterVertically, horizontalArrangement = Arrangement.spacedBy(MamaSpacing.s)) {
        Text(
            text = text,
            style = MaterialTheme.typography.headlineMedium,
            color = MaterialTheme.colorScheme.onBackground,
            textAlign = TextAlign.Center,
            modifier = Modifier.semantics { liveRegion = LiveRegionMode.Polite },
        )
        if (phase == HomePhase.Processing) BouncingDots()
    }
}

/** Sent, Mamá's call, message home: filled as far as the real progress goes. */
@Composable
private fun StepBars(progress: TriggerProgress) {
    val animate = rememberAnimationsEnabled()
    val labels = listOf("Enviado", "Llamada de Mamá", "Mensaje a casa")
    // Sending: the first bar works. Active: it is done and the second one works.
    val done = if (progress == TriggerProgress.Active) 1 else 0
    Row(modifier = Modifier.fillMaxWidth(), horizontalArrangement = Arrangement.spacedBy(MamaSpacing.s)) {
        labels.forEachIndexed { index, label ->
            Column(modifier = Modifier.weight(1f), verticalArrangement = Arrangement.spacedBy(MamaSpacing.xs)) {
                val modifier = Modifier.fillMaxWidth()
                val color = MaterialTheme.colorScheme.primary
                val track = MaterialTheme.colorScheme.outlineVariant
                when {
                    index < done -> LinearProgressIndicator(progress = { 1f }, modifier = modifier, color = color, trackColor = track)
                    index == done && animate -> LinearProgressIndicator(modifier = modifier, color = color, trackColor = track)
                    index == done -> LinearProgressIndicator(progress = { 0.5f }, modifier = modifier, color = color, trackColor = track)
                    else -> LinearProgressIndicator(progress = { 0f }, modifier = modifier, color = color, trackColor = track)
                }
                Text(
                    text = label,
                    style = MaterialTheme.typography.labelSmall,
                    color = MaterialTheme.colorScheme.onSurfaceVariant,
                )
            }
        }
    }
}

@Composable
private fun ElapsedPill(elapsed: String) {
    Surface(
        shape = CircleShape,
        color = MaterialTheme.colorScheme.primaryContainer,
        contentColor = MaterialTheme.colorScheme.onPrimaryContainer,
    ) {
        Row(
            modifier = Modifier.padding(horizontal = MamaSpacing.m, vertical = MamaSpacing.s),
            verticalAlignment = Alignment.CenterVertically,
            horizontalArrangement = Arrangement.spacedBy(MamaSpacing.s),
        ) {
            BlinkingDot()
            Text(text = elapsed, style = MaterialTheme.typography.titleMedium)
        }
    }
}

@Composable
private fun FailureRow(failure: HomeFailure) {
    val reason = when (failure) {
        HomeFailure.NotStarted -> "No se pudo llamar. Revisa los permisos en Ajustes."
        HomeFailure.NotDelivered -> "No se pudo llamar. Revisa tu conexión e inténtalo otra vez."
    }
    Card(
        modifier = Modifier.fillMaxWidth().semantics { liveRegion = LiveRegionMode.Polite },
        colors = CardDefaults.cardColors(
            containerColor = MaterialTheme.colorScheme.errorContainer,
            contentColor = MaterialTheme.colorScheme.onErrorContainer,
        ),
    ) {
        Text(
            text = reason,
            style = MaterialTheme.typography.bodyLarge,
            modifier = Modifier.padding(MamaSpacing.m),
        )
    }
}

/** Discreet on purpose, like on the trigger screen: a text button with a quiet caption. */
@Composable
private fun CancelBar(onCancel: () -> Unit) {
    Column(
        modifier = Modifier
            .fillMaxWidth()
            .windowInsetsPadding(WindowInsets.safeDrawing.only(WindowInsetsSides.Bottom))
            .padding(horizontal = MamaSpacing.screen, vertical = MamaSpacing.m),
        horizontalAlignment = Alignment.CenterHorizontally,
    ) {
        TextButton(onClick = onCancel) { Text("Fue sin querer") }
        Text(
            text = "Tócalo solo si fue un error.",
            style = MaterialTheme.typography.bodySmall,
            color = MaterialTheme.colorScheme.onSurfaceVariant,
            textAlign = TextAlign.Center,
        )
    }
}

@Composable
private fun PreviewHome(state: HomeState) {
    MamaTheme {
        HomeScreen(
            state = state,
            onPressStart = {},
            onPressEnd = {},
            onActivate = {},
            onCancel = {},
            onOpenProfile = {},
        )
    }
}

@Preview(showBackground = true, heightDp = 900)
@Composable
private fun HomeIdlePreview() = PreviewHome(HomeState())

@Preview(showBackground = true, heightDp = 900, uiMode = Configuration.UI_MODE_NIGHT_YES)
@Composable
private fun HomeIdleDarkPreview() = PreviewHome(HomeState())

@Preview(showBackground = true, heightDp = 900)
@Composable
private fun HomeArmingPreview() = PreviewHome(HomeState(phase = HomePhase.Arming, armProgress = 0.6f))

@Preview(showBackground = true, heightDp = 900)
@Composable
private fun HomeProcessingPreview() =
    PreviewHome(HomeState(phase = HomePhase.Processing, armProgress = 1f, progress = TriggerProgress.Sending))

@Preview(showBackground = true, heightDp = 900)
@Composable
private fun HomeListeningPreview() =
    PreviewHome(HomeState(phase = HomePhase.Listening, listeningSeconds = 83, progress = TriggerProgress.Active))

@Preview(showBackground = true, heightDp = 900)
@Composable
private fun HomeCancelledPreview() = PreviewHome(HomeState(phase = HomePhase.Cancelled))

@Preview(showBackground = true, heightDp = 900)
@Composable
private fun HomeFailedPreview() = PreviewHome(HomeState(phase = HomePhase.Failed, failure = HomeFailure.NotStarted))
