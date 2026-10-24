package com.team27.mama.ui.components

import androidx.compose.animation.core.RepeatMode
import androidx.compose.animation.core.animateFloat
import androidx.compose.animation.core.infiniteRepeatable
import androidx.compose.animation.core.rememberInfiniteTransition
import androidx.compose.animation.core.tween
import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.layout.size
import androidx.compose.foundation.lazy.LazyColumn
import androidx.compose.foundation.lazy.itemsIndexed
import androidx.compose.foundation.lazy.rememberLazyListState
import androidx.compose.material3.Card
import androidx.compose.material3.CardDefaults
import androidx.compose.material3.Icon
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.Text
import androidx.compose.runtime.Composable
import androidx.compose.runtime.LaunchedEffect
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.res.painterResource
import androidx.compose.ui.text.SpanStyle
import androidx.compose.ui.text.buildAnnotatedString
import androidx.compose.ui.text.withStyle
import androidx.compose.ui.unit.dp
import com.team27.mama.R
import com.team27.mama.call.Caption
import com.team27.mama.call.Speaker
import com.team27.mama.ui.theme.MamaSpacing

/**
 * The "Subtítulos en vivo" panel (#94): one `Mamá:` / `Tú:` line per turn,
 * the line being spoken in full color with a blinking cursor. It always shows
 * the newest line. Bystander rule: every line looks the same, whatever it
 * says; no phrase is ever highlighted.
 */
@Composable
fun CaptionsPanel(captions: List<Caption>, modifier: Modifier = Modifier) {
    // Reversed list in a reverse layout: the newest line sits at the bottom and
    // stays in view while it grows, the usual chat pattern.
    val listState = rememberLazyListState()
    LaunchedEffect(captions.size) {
        if (captions.isNotEmpty()) listState.animateScrollToItem(0)
    }
    Card(
        modifier = modifier,
        shape = MaterialTheme.shapes.large,
        colors = CardDefaults.cardColors(containerColor = MaterialTheme.colorScheme.surfaceContainerLow),
    ) {
        Column(
            modifier = Modifier.padding(MamaSpacing.l),
            verticalArrangement = Arrangement.spacedBy(MamaSpacing.m),
        ) {
            Row(
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
                    text = "SUBTÍTULOS EN VIVO",
                    style = MaterialTheme.typography.titleSmall,
                    color = MaterialTheme.colorScheme.primary,
                )
            }
            LazyColumn(
                state = listState,
                reverseLayout = true,
                verticalArrangement = Arrangement.spacedBy(12.dp, Alignment.Top),
                modifier = Modifier.fillMaxWidth(),
            ) {
                itemsIndexed(captions.asReversed()) { index, caption ->
                    CaptionLine(caption, current = index == 0)
                }
            }
        }
    }
}

@Composable
private fun CaptionLine(caption: Caption, current: Boolean) {
    val colors = MaterialTheme.colorScheme
    val label = MaterialTheme.typography.titleMedium.toSpanStyle().copy(color = colors.onSurface)
    val cursorAlpha = if (caption.partial) {
        rememberInfiniteTransition(label = "caption-cursor").animateFloat(
            initialValue = 1f,
            targetValue = 0f,
            animationSpec = infiniteRepeatable(tween(530), RepeatMode.Reverse),
            label = "cursor",
        ).value
    } else {
        0f
    }
    Text(
        style = MaterialTheme.typography.bodyLarge,
        text = buildAnnotatedString {
            withStyle(label) { append(if (caption.speaker == Speaker.Mama) "Mamá: " else "Tú: ") }
            withStyle(SpanStyle(color = if (current) colors.onSurface else colors.onSurfaceVariant)) {
                append(caption.text)
            }
            if (caption.partial) {
                withStyle(SpanStyle(color = colors.primary.copy(alpha = cursorAlpha))) { append("|") }
            }
        },
    )
}
