package com.team27.mama.ui.components

import androidx.compose.animation.core.animateFloatAsState
import androidx.compose.animation.core.tween
import androidx.compose.foundation.background
import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Box
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.height
import androidx.compose.foundation.layout.width
import androidx.compose.foundation.shape.CircleShape
import androidx.compose.material3.MaterialTheme
import androidx.compose.runtime.Composable
import androidx.compose.runtime.getValue
import androidx.compose.runtime.remember
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.unit.dp
import com.team27.mama.call.Speaker
import kotlin.random.Random

// Resting silhouette of the wave, tallest in the middle like a voice.
private val barShape = listOf(0.35f, 0.6f, 1f, 0.55f, 0.85f, 0.5f, 0.9f, 0.6f, 0.3f)

/**
 * The call's audio wave (#94): bars that follow the voice [level] of whoever is
 * [speaking], primary for Mamá and secondary for you, and settle into small
 * dots in the silence between turns. Decoration only; TalkBack skips it.
 */
@Composable
fun AudioWave(speaking: Speaker, level: Float, modifier: Modifier = Modifier) {
    val color = when (speaking) {
        Speaker.You -> MaterialTheme.colorScheme.secondary
        else -> MaterialTheme.colorScheme.primary
    }
    // New jitter on every level update, so the bars do not move in lockstep.
    val jitter = remember(level) { List(barShape.size) { Random.nextFloat() * 0.5f + 0.5f } }
    Row(
        modifier = modifier.height(40.dp),
        horizontalArrangement = Arrangement.spacedBy(6.dp),
        verticalAlignment = Alignment.CenterVertically,
    ) {
        barShape.forEachIndexed { index, shape ->
            val target = if (speaking == Speaker.None) 0.15f else 0.2f + 0.8f * level.coerceIn(0f, 1f) * shape * jitter[index]
            val height by animateFloatAsState(target, tween(110), label = "wave-bar")
            Box(
                Modifier
                    .width(6.dp)
                    .height(40.dp * height)
                    .background(color, CircleShape),
            )
        }
    }
}
