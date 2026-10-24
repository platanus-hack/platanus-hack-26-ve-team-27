package com.team27.mama.ui.components

import android.content.res.Configuration
import android.provider.Settings
import androidx.annotation.DrawableRes
import androidx.compose.animation.Crossfade
import androidx.compose.animation.core.Animatable
import androidx.compose.animation.core.LinearEasing
import androidx.compose.animation.core.RepeatMode
import androidx.compose.animation.core.Spring
import androidx.compose.animation.core.animateFloat
import androidx.compose.animation.core.infiniteRepeatable
import androidx.compose.animation.core.rememberInfiniteTransition
import androidx.compose.animation.core.spring
import androidx.compose.animation.core.tween
import androidx.compose.foundation.Image
import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.layout.size
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.Surface
import androidx.compose.runtime.Composable
import androidx.compose.runtime.LaunchedEffect
import androidx.compose.runtime.State
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.remember
import androidx.compose.runtime.setValue
import androidx.compose.ui.Modifier
import androidx.compose.ui.draw.drawWithContent
import androidx.compose.ui.geometry.Offset
import androidx.compose.ui.graphics.drawscope.withTransform
import androidx.compose.ui.platform.LocalContext
import androidx.compose.ui.platform.LocalInspectionMode
import androidx.compose.ui.res.painterResource
import androidx.compose.ui.tooling.preview.Preview
import androidx.compose.ui.unit.Dp
import androidx.compose.ui.unit.dp
import com.team27.mama.R
import com.team27.mama.ui.theme.MamaSpacing
import com.team27.mama.ui.theme.MamaTheme
import kotlinx.coroutines.delay
import kotlin.random.Random

/*
 * The Mamá character (#72): a risograph bust in a chartreuse circle, one
 * drawable per pose (res/drawable-nodpi, 512 px, transparent). Every pose is
 * cut to the same circle size and position, so swapping them never makes her
 * jump. She is decoration: screens say what is happening in text, and TalkBack
 * skips her.
 *
 * Motion: a slow breath (scale and a tiny sway, anchored at the bottom like a
 * bust), a springy pop with a crossfade when the pose changes, and a blink on
 * the resting pose once a blink frame exists. With system animations off she
 * stands still.
 *
 * Bystander rule (app/UI_RULES.md): the trigger screen may only show the
 * neutral poses. [MamaPose.Typing] reads as "she is texting someone", so it
 * stays on screens nobody else is meant to read.
 */

enum class MamaPose(@DrawableRes val drawable: Int) {
    /** Resting, a knowing smile. */
    Idle(R.drawable.mama_pendiente),

    /** Raised eyebrow, "¿ajá?". */
    Listening(R.drawable.mama_escuchando),

    /** Wide eyes, just noticed something. */
    Alert(R.drawable.mama_despierta),

    /** Phone to her ear. */
    Calling(R.drawable.mama_llamando),

    /** Looking down at her phone. Never on the trigger screen. */
    Typing(R.drawable.mama_escribiendo),

    /** Eyes closed, hand on her chest. */
    Relieved(R.drawable.mama_lista),
}

// Eyes-closed frame of [MamaPose.Idle]. Set it to R.drawable.mama_parpadeo once
// that drawable lands; null means no blink.
@DrawableRes
private val idleBlinkFrame: Int? = null

@Composable
fun MamaCharacter(pose: MamaPose, modifier: Modifier = Modifier, size: Dp = 160.dp) {
    val animate = rememberAnimationsEnabled()

    val pop = remember { Animatable(1f) }
    LaunchedEffect(pose) {
        if (!animate) return@LaunchedEffect
        pop.snapTo(0.9f)
        pop.animateTo(1f, spring(dampingRatio = 0.45f, stiffness = Spring.StiffnessMediumLow))
    }

    // Read only inside drawWithContent below: each frame is then a redraw, with no
    // recomposition. Not a graphicsLayer: Compose reports a layer's scale or rotation
    // as a layout change, which sends ~10 accessibility events a second to every
    // service on, MAMÁ's own trigger service included.
    var breath: State<Float>? = null
    var sway: State<Float>? = null
    if (animate) {
        val transition = rememberInfiniteTransition(label = "mama-breath")
        breath = transition.animateFloat(
            initialValue = 1f,
            targetValue = 1.025f,
            animationSpec = infiniteRepeatable(tween(1500, easing = LinearEasing), RepeatMode.Reverse),
            label = "breath",
        )
        // A different period than the breath, so the two never line up into a loop you notice.
        sway = transition.animateFloat(
            initialValue = -1f,
            targetValue = 1f,
            animationSpec = infiniteRepeatable(tween(2100), RepeatMode.Reverse),
            label = "sway",
        )
    }

    var blinking by remember { mutableStateOf(false) }
    val blinkFrame = idleBlinkFrame
    if (animate && blinkFrame != null && pose == MamaPose.Idle) {
        LaunchedEffect(Unit) {
            while (true) {
                delay(Random.nextLong(3000, 6000))
                blinking = true
                delay(130)
                blinking = false
            }
        }
    }

    Crossfade(
        targetState = pose,
        animationSpec = tween(if (animate) 220 else 0),
        modifier = modifier
            .size(size)
            .drawWithContent {
                val scale = (breath?.value ?: 1f) * pop.value
                // Bottom center: she breathes and sways from her shoulders.
                val pivot = Offset(this.size.width / 2f, this.size.height)
                withTransform({
                    rotate(sway?.value ?: 0f, pivot)
                    scale(scale, scale, pivot)
                }) {
                    this@drawWithContent.drawContent()
                }
            },
        label = "mama-pose",
    ) { shown ->
        val frame = if (shown == MamaPose.Idle && blinking && blinkFrame != null) blinkFrame else shown.drawable
        Image(painter = painterResource(frame), contentDescription = null)
    }
}

/** False when the user turned animations off (animator duration scale 0). */
@Composable
private fun rememberAnimationsEnabled(): Boolean {
    if (LocalInspectionMode.current) return true
    val resolver = LocalContext.current.contentResolver
    return remember(resolver) {
        Settings.Global.getFloat(resolver, Settings.Global.ANIMATOR_DURATION_SCALE, 1f) > 0f
    }
}

@Preview(showBackground = true)
@Composable
private fun MamaCharacterPosesPreview() {
    MamaTheme { PosesGrid() }
}

@Preview(showBackground = true, uiMode = Configuration.UI_MODE_NIGHT_YES)
@Composable
private fun MamaCharacterPosesDarkPreview() {
    MamaTheme { PosesGrid() }
}

@Composable
private fun PosesGrid() {
    Surface(color = MaterialTheme.colorScheme.background) {
        Column(
            modifier = Modifier.padding(MamaSpacing.m),
            verticalArrangement = Arrangement.spacedBy(MamaSpacing.m),
        ) {
            MamaPose.entries.chunked(3).forEach { row ->
                Row(horizontalArrangement = Arrangement.spacedBy(MamaSpacing.m)) {
                    row.forEach { MamaCharacter(pose = it, size = 96.dp) }
                }
            }
        }
    }
}
