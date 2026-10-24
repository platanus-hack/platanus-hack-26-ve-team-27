package com.team27.mama.ui.screens

import android.content.res.Configuration
import androidx.compose.animation.core.LinearEasing
import androidx.compose.animation.core.RepeatMode
import androidx.compose.animation.core.animateFloat
import androidx.compose.animation.core.infiniteRepeatable
import androidx.compose.animation.core.rememberInfiniteTransition
import androidx.compose.animation.core.tween
import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Box
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.Spacer
import androidx.compose.foundation.layout.WindowInsets
import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.height
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.layout.safeDrawing
import androidx.compose.foundation.layout.size
import androidx.compose.foundation.layout.windowInsetsPadding
import androidx.compose.material3.Icon
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.Surface
import androidx.compose.material3.Text
import androidx.compose.runtime.Composable
import androidx.compose.runtime.remember
import androidx.compose.runtime.rememberCoroutineScope
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.draw.drawBehind
import androidx.compose.ui.graphics.drawscope.Stroke
import androidx.compose.ui.res.painterResource
import androidx.compose.ui.tooling.preview.Preview
import androidx.compose.ui.unit.dp
import com.team27.mama.R
import com.team27.mama.call.CallSession
import com.team27.mama.call.FakeCallSession
import com.team27.mama.ui.components.CallButton
import com.team27.mama.ui.components.MamaCharacter
import com.team27.mama.ui.components.MamaPose
import com.team27.mama.ui.theme.MamaSpacing
import com.team27.mama.ui.theme.MamaTheme
import com.team27.mama.ui.theme.hangUp
import com.team27.mama.ui.theme.onHangUp

/*
 * Mamá's call ringing inside the app (#94, design D). It has to pass for the
 * phone's own incoming-call screen: no wordmark, no word about why she calls.
 * Contestar opens InCallScreen; Rechazar cancels the incident like "Fue sin
 * querer" does on the trigger screen, and says nothing about it.
 */

@Composable
fun IncomingCallScreen(session: CallSession, onAnswer: () -> Unit, onCancel: () -> Unit) {
    Surface(modifier = Modifier.fillMaxSize(), color = MaterialTheme.colorScheme.background) {
        Column(
            modifier = Modifier
                .fillMaxSize()
                .windowInsetsPadding(WindowInsets.safeDrawing)
                .padding(horizontal = MamaSpacing.screen, vertical = MamaSpacing.xl),
            horizontalAlignment = Alignment.CenterHorizontally,
        ) {
            Spacer(Modifier.weight(1f))
            Text(
                text = "Llamada entrante",
                style = MaterialTheme.typography.titleLarge,
                color = MaterialTheme.colorScheme.primary,
            )
            Text(
                text = "Mamá",
                style = MaterialTheme.typography.displayMedium,
                color = MaterialTheme.colorScheme.onBackground,
            )
            Text(
                text = "Móvil",
                style = MaterialTheme.typography.titleMedium,
                color = MaterialTheme.colorScheme.onSurfaceVariant,
            )
            Spacer(Modifier.height(MamaSpacing.l))
            Box(modifier = Modifier.ringing(), contentAlignment = Alignment.Center) {
                MamaCharacter(pose = MamaPose.Calling, size = 220.dp)
            }
            Spacer(Modifier.height(MamaSpacing.l))
            CaptionsOnChip()
            Spacer(Modifier.weight(2f))
            Row(
                modifier = Modifier
                    .fillMaxWidth()
                    .padding(horizontal = MamaSpacing.l),
                horizontalArrangement = Arrangement.SpaceBetween,
            ) {
                CallButton(
                    label = "Rechazar",
                    icon = R.drawable.ic_call_end,
                    containerColor = MaterialTheme.colorScheme.hangUp,
                    contentColor = MaterialTheme.colorScheme.onHangUp,
                    onClick = {
                        session.hangUp()
                        onCancel()
                    },
                )
                CallButton(
                    label = "Contestar",
                    icon = R.drawable.ic_call,
                    containerColor = MaterialTheme.colorScheme.primary,
                    contentColor = MaterialTheme.colorScheme.onPrimary,
                    onClick = {
                        session.answer()
                        onAnswer()
                    },
                )
            }
        }
    }
}

/** Two soft rings that breathe out from Mamá while the phone rings. */
@Composable
private fun Modifier.ringing(): Modifier {
    val inner = MaterialTheme.colorScheme.primaryContainer
    val outer = MaterialTheme.colorScheme.outlineVariant
    val pulse = rememberInfiniteTransition(label = "ringing").animateFloat(
        initialValue = 0f,
        targetValue = 1f,
        animationSpec = infiniteRepeatable(tween(1600, easing = LinearEasing), RepeatMode.Reverse),
        label = "ring",
    ).value
    return drawBehind {
        val radius = size.minDimension / 2
        drawCircle(inner, radius = radius * (1.42f + 0.04f * pulse), style = Stroke(2.dp.toPx()))
        drawCircle(outer, radius = radius * (1.7f + 0.05f * pulse), alpha = 0.6f - 0.3f * pulse, style = Stroke(1.dp.toPx()))
    }
}

/** "Live captions on", the way a phone shows an accessibility setting. */
@Composable
private fun CaptionsOnChip() {
    Surface(shape = MaterialTheme.shapes.large, color = MaterialTheme.colorScheme.surfaceContainer) {
        Row(
            modifier = Modifier.padding(horizontal = MamaSpacing.m, vertical = MamaSpacing.s),
            verticalAlignment = Alignment.CenterVertically,
            horizontalArrangement = Arrangement.spacedBy(MamaSpacing.s),
        ) {
            Icon(
                painterResource(R.drawable.ic_subtitles),
                contentDescription = null,
                tint = MaterialTheme.colorScheme.primary,
                modifier = Modifier.size(20.dp),
            )
            Text(
                text = "Subtítulos en vivo activados",
                style = MaterialTheme.typography.bodyLarge,
                color = MaterialTheme.colorScheme.onSurfaceVariant,
            )
        }
    }
}

@Composable
private fun rememberPreviewSession(): CallSession {
    val scope = rememberCoroutineScope()
    return remember { FakeCallSession(scope) }
}

@Preview(showBackground = true, heightDp = 800)
@Composable
private fun IncomingCallScreenPreview() {
    MamaTheme { IncomingCallScreen(rememberPreviewSession(), onAnswer = {}, onCancel = {}) }
}

@Preview(showBackground = true, heightDp = 800, uiMode = Configuration.UI_MODE_NIGHT_YES)
@Composable
private fun IncomingCallScreenDarkPreview() {
    MamaTheme { IncomingCallScreen(rememberPreviewSession(), onAnswer = {}, onCancel = {}) }
}
