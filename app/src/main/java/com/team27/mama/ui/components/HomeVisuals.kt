package com.team27.mama.ui.components

import androidx.compose.animation.core.FastOutSlowInEasing
import androidx.compose.animation.core.LinearEasing
import androidx.compose.animation.core.RepeatMode
import androidx.compose.animation.core.animateFloat
import androidx.compose.animation.core.infiniteRepeatable
import androidx.compose.animation.core.rememberInfiniteTransition
import androidx.compose.animation.core.tween
import androidx.compose.foundation.Canvas
import androidx.compose.foundation.layout.height
import androidx.compose.foundation.layout.size
import androidx.compose.foundation.layout.width
import androidx.compose.material3.MaterialTheme
import androidx.compose.runtime.Composable
import androidx.compose.runtime.LaunchedEffect
import androidx.compose.runtime.State
import androidx.compose.runtime.mutableLongStateOf
import androidx.compose.runtime.remember
import androidx.compose.runtime.withFrameMillis
import androidx.compose.ui.Modifier
import androidx.compose.ui.geometry.CornerRadius
import androidx.compose.ui.geometry.Offset
import androidx.compose.ui.geometry.Size
import androidx.compose.ui.graphics.Color
import androidx.compose.ui.graphics.StrokeCap
import androidx.compose.ui.graphics.drawscope.DrawScope
import androidx.compose.ui.graphics.drawscope.Stroke
import androidx.compose.ui.unit.dp
import com.team27.mama.ui.home.HomePhase
import kotlin.math.PI
import kotlin.math.abs
import kotlin.math.cos
import kotlin.math.floor
import kotlin.math.sin

/*
 * The moving parts of the home screen (HomeScreen.kt): the halo around Mamá's portrait, the
 * scrolling voice waveform under it and two tiny indicators. Every one is drawn on a Canvas from
 * MaterialTheme.colorScheme and stands still when system animations are off
 * (rememberAnimationsEnabled).
 *
 * None of it is audio. There is no microphone recording in the app: the "voice" is a smooth,
 * repeatable pseudo-random envelope that only suggests that Mamá is listening.
 */

/** Size of the halo box; the portrait (196 dp) sits in its middle. */
val HomeHaloSize = 280.dp

private const val PORTRAIT_RADIUS_DP = 98f
private const val RING_RADIUS_DP = 124f
private const val BAR_COUNT = 64

/** Milliseconds from the frame clock while [running], a frozen mid-point otherwise (so a static frame still has bars). */
@Composable
private fun rememberFrameMillis(running: Boolean): State<Long> {
    val time = remember { mutableLongStateOf(FROZEN_MS) }
    LaunchedEffect(running) {
        if (!running) return@LaunchedEffect
        val start = withFrameMillis { it }
        while (true) {
            withFrameMillis { time.longValue = FROZEN_MS + it - start }
        }
    }
    return time
}

private const val FROZEN_MS = 3_600L

/** A repeatable value in 0..1 for [k]. */
private fun hash01(k: Long): Float = (((k * 2654435761L) ushr 8) and 0xFFFF).toFloat() / 0xFFFF

/** A smooth "voice" level in 0..1 at bar [k]: slow phrases, quick syllables and a little grain. */
private fun voiceLevel(k: Long): Float {
    val syllable = 0.5f + 0.5f * sin(k * 0.45f) * sin(k * 0.131f + 1f)
    val phrase = (0.5f + 0.5f * sin(k * 0.07f)).let { it * it * (3f - 2f * it) }
    return ((0.3f + 0.7f * phrase) * (0.5f * syllable + 0.5f * hash01(k))).coerceIn(0f, 1f)
}

/**
 * What surrounds the portrait for each [phase]: a thin ring and a breathing one when idle, the
 * filling arc while it is held, spinning arcs while the call is placed, ripples and a ring of
 * voice bars while Mamá listens.
 */
@Composable
fun HomeHalo(phase: HomePhase, armProgress: Float, modifier: Modifier = Modifier) {
    val animate = rememberAnimationsEnabled()
    val scheme = MaterialTheme.colorScheme
    val primary = scheme.primary
    val soft = scheme.primaryContainer
    val track = scheme.outlineVariant

    val transition = rememberInfiniteTransition(label = "home-halo")
    val breath = transition.animateFloat(
        initialValue = 0f,
        targetValue = 1f,
        animationSpec = infiniteRepeatable(tween(2400, easing = FastOutSlowInEasing), RepeatMode.Reverse),
        label = "breath",
    )
    val spin = transition.animateFloat(
        initialValue = 0f,
        targetValue = 360f,
        animationSpec = infiniteRepeatable(tween(1100, easing = LinearEasing)),
        label = "spin",
    )
    val sweep = transition.animateFloat(
        initialValue = 70f,
        targetValue = 200f,
        animationSpec = infiniteRepeatable(tween(900, easing = FastOutSlowInEasing), RepeatMode.Reverse),
        label = "sweep",
    )
    val ripple = transition.animateFloat(
        initialValue = 0f,
        targetValue = 1f,
        animationSpec = infiniteRepeatable(tween(2400, easing = LinearEasing)),
        label = "ripple",
    )
    val voice = rememberFrameMillis(running = animate && phase == HomePhase.Listening)

    Canvas(modifier = modifier.size(HomeHaloSize)) {
        val dp = 1.dp.toPx()
        val center = Offset(size.width / 2f, size.height / 2f)
        // Each value is read here, inside the draw, so a frame redraws without recomposing.
        val breathNow = if (animate) breath.value else 0f
        when (phase) {
            HomePhase.Idle, HomePhase.Failed, HomePhase.Cancelled -> {
                drawCircle(track, radius = 136 * dp, center = center, style = Stroke(2 * dp))
                drawCircle(
                    primary.copy(alpha = 0.15f + 0.25f * breathNow),
                    radius = (126 + 6 * breathNow) * dp,
                    center = center,
                    style = Stroke(2 * dp),
                )
            }
            HomePhase.Arming -> {
                drawRing(track, center, dp)
                drawArc(
                    color = primary,
                    startAngle = -90f,
                    sweepAngle = 360f * armProgress,
                    useCenter = false,
                    topLeft = center - Offset(RING_RADIUS_DP * dp, RING_RADIUS_DP * dp),
                    size = Size(2 * RING_RADIUS_DP * dp, 2 * RING_RADIUS_DP * dp),
                    style = Stroke(8 * dp, cap = StrokeCap.Round),
                )
            }
            HomePhase.Processing -> {
                drawRing(track, center, dp)
                val rotation = if (animate) spin.value else -90f
                val length = if (animate) sweep.value else 120f
                drawRingArc(primary, rotation, length, center, dp)
                drawRingArc(soft, rotation + 180f, length * 0.45f, center, dp)
            }
            HomePhase.Listening -> {
                val ms = voice.value
                for (i in 0 until 3) {
                    val p = if (animate) (ripple.value + i / 3f) % 1f else (i + 1) / 4f
                    drawCircle(
                        primary.copy(alpha = 0.5f * (1f - p)),
                        radius = (PORTRAIT_RADIUS_DP + 4 + 36 * p) * dp,
                        center = center,
                        style = Stroke(2 * dp),
                    )
                }
                drawVoiceBars(primary, center, ms, dp)
            }
        }
    }
}

private fun DrawScope.drawRing(color: Color, center: Offset, dp: Float) {
    drawCircle(color, radius = RING_RADIUS_DP * dp, center = center, style = Stroke(8 * dp))
}

private fun DrawScope.drawRingArc(
    color: Color,
    start: Float,
    sweepAngle: Float,
    center: Offset,
    dp: Float,
) {
    drawArc(
        color = color,
        startAngle = start,
        sweepAngle = sweepAngle,
        useCenter = false,
        topLeft = center - Offset(RING_RADIUS_DP * dp, RING_RADIUS_DP * dp),
        size = Size(2 * RING_RADIUS_DP * dp, 2 * RING_RADIUS_DP * dp),
        style = Stroke(8 * dp, cap = StrokeCap.Round),
    )
}

/** [BAR_COUNT] short bars around the portrait, each level following a smooth envelope over time. */
private fun DrawScope.drawVoiceBars(color: Color, center: Offset, ms: Long, dp: Float) {
    val inner = (PORTRAIT_RADIUS_DP + 10) * dp
    val gate = 0.6f + 0.4f * sin(ms * 0.0011f).let { it * it }
    for (i in 0 until BAR_COUNT) {
        val angle = (i * 2.0 * PI / BAR_COUNT - PI / 2).toFloat()
        val wave = 0.5f + 0.5f * (0.6f * sin(ms * 0.0047f + i * 1.7f) + 0.4f * sin(ms * 0.0021f - i * 0.43f))
        val length = (4f + 20f * wave * gate) * dp
        val from = Offset(center.x + cos(angle) * inner, center.y + sin(angle) * inner)
        val to = Offset(center.x + cos(angle) * (inner + length), center.y + sin(angle) * (inner + length))
        drawLine(color.copy(alpha = 0.8f), from, to, strokeWidth = 3 * dp, cap = StrokeCap.Round)
    }
}

/**
 * A voice-memo waveform: rounded bars 4 dp wide with a 3 dp gap, a new one every [BAR_EVERY_MS]
 * entering from the right and the older ones fading out to the left. DECORATIVE: the levels come
 * from [voiceLevel], not from a microphone. The scroll is continuous (the bars move with the frame
 * clock between steps), so it reads smooth and not stepped.
 */
@Composable
fun VoiceWaveform(modifier: Modifier = Modifier) {
    val animate = rememberAnimationsEnabled()
    val color = MaterialTheme.colorScheme.primary
    val time = rememberFrameMillis(running = animate)
    Canvas(modifier = modifier.width(HomeHaloSize).height(112.dp)) {
        val dp = 1.dp.toPx()
        val barWidth = 4 * dp
        val pitch = 7 * dp
        val head = time.value.toDouble() / BAR_EVERY_MS
        val newest = floor(head).toLong()
        val count = (size.width / pitch).toInt() + 2
        for (j in 0..count) {
            val k = newest - j
            // The newest bar starts just outside the right edge and slides in by one pitch per step.
            val x = size.width - ((head - k) * pitch).toFloat()
            if (x + barWidth < 0f || x > size.width) continue
            val fade = (x / size.width).coerceIn(0f, 1f)
            val level = voiceLevel(k)
            val barHeight = (12f + 100f * level) * dp * 0.96f
            drawRoundRect(
                color = color.copy(alpha = fade * fade),
                topLeft = Offset(x, (size.height - barHeight) / 2f),
                size = Size(barWidth, barHeight),
                cornerRadius = CornerRadius(barWidth / 2f),
            )
        }
    }
}

private const val BAR_EVERY_MS = 60

/** Three dots that bounce one after the other, next to "Llamando a Mamá". */
@Composable
fun BouncingDots(modifier: Modifier = Modifier) {
    val animate = rememberAnimationsEnabled()
    val color = MaterialTheme.colorScheme.primary
    val phase = rememberInfiniteTransition(label = "dots").animateFloat(
        initialValue = 0f,
        targetValue = 1f,
        animationSpec = infiniteRepeatable(tween(1000, easing = LinearEasing)),
        label = "dots-phase",
    )
    Canvas(modifier = modifier.width(32.dp).height(16.dp)) {
        val dp = 1.dp.toPx()
        for (i in 0 until 3) {
            val bounce = if (animate) abs(sin(((phase.value - i * 0.15f) * PI).toFloat())) else 0f
            drawCircle(
                color,
                radius = 3 * dp,
                center = Offset((4 + i * 12) * dp, size.height - 4 * dp - 8 * dp * bounce),
            )
        }
    }
}

/** The small recording dot of the elapsed-time pill; it blinks. */
@Composable
fun BlinkingDot(modifier: Modifier = Modifier) {
    val animate = rememberAnimationsEnabled()
    val color = MaterialTheme.colorScheme.primary
    val alpha = rememberInfiniteTransition(label = "blink").animateFloat(
        initialValue = 1f,
        targetValue = 0.2f,
        animationSpec = infiniteRepeatable(tween(700, easing = LinearEasing), RepeatMode.Reverse),
        label = "blink-alpha",
    )
    Canvas(modifier = modifier.size(8.dp)) {
        drawCircle(color.copy(alpha = if (animate) alpha.value else 1f))
    }
}
