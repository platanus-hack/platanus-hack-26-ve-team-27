package com.team27.mama.ui.components

import androidx.annotation.DrawableRes
import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.size
import androidx.compose.material3.FilledIconButton
import androidx.compose.material3.FilledIconToggleButton
import androidx.compose.material3.Icon
import androidx.compose.material3.IconButtonDefaults
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.Text
import androidx.compose.runtime.Composable
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.graphics.Color
import androidx.compose.ui.res.painterResource
import androidx.compose.ui.semantics.clearAndSetSemantics
import androidx.compose.ui.unit.Dp
import androidx.compose.ui.unit.dp
import com.team27.mama.ui.theme.MamaSpacing

/*
 * The round buttons of the in-app call (#94), each with its label underneath,
 * like the phone's own call screen. The icon carries the label for TalkBack,
 * so the visible text is skipped.
 */

@Composable
fun CallButton(
    label: String,
    @DrawableRes icon: Int,
    containerColor: Color,
    contentColor: Color,
    onClick: () -> Unit,
    modifier: Modifier = Modifier,
    size: Dp = 72.dp,
) {
    LabeledCallControl(label, modifier) {
        FilledIconButton(
            onClick = onClick,
            modifier = Modifier.size(size),
            colors = IconButtonDefaults.filledIconButtonColors(
                containerColor = containerColor,
                contentColor = contentColor,
            ),
        ) {
            Icon(painterResource(icon), contentDescription = label, modifier = Modifier.size(size / 2.25f))
        }
    }
}

/** On/off call control (Silenciar, Altavoz): surface when off, primary when on. */
@Composable
fun CallToggleButton(
    label: String,
    @DrawableRes icon: Int,
    checked: Boolean,
    onCheckedChange: (Boolean) -> Unit,
    modifier: Modifier = Modifier,
    size: Dp = 56.dp,
) {
    LabeledCallControl(label, modifier) {
        FilledIconToggleButton(
            checked = checked,
            onCheckedChange = onCheckedChange,
            modifier = Modifier.size(size),
            colors = IconButtonDefaults.filledIconToggleButtonColors(
                containerColor = MaterialTheme.colorScheme.surfaceContainerHighest,
                contentColor = MaterialTheme.colorScheme.onSurface,
                checkedContainerColor = MaterialTheme.colorScheme.primary,
                checkedContentColor = MaterialTheme.colorScheme.onPrimary,
            ),
        ) {
            Icon(painterResource(icon), contentDescription = label, modifier = Modifier.size(28.dp))
        }
    }
}

@Composable
private fun LabeledCallControl(label: String, modifier: Modifier, button: @Composable () -> Unit) {
    Column(
        modifier = modifier,
        horizontalAlignment = Alignment.CenterHorizontally,
        verticalArrangement = Arrangement.spacedBy(MamaSpacing.s),
    ) {
        button()
        Text(
            text = label,
            style = MaterialTheme.typography.bodyLarge,
            color = MaterialTheme.colorScheme.onSurfaceVariant,
            modifier = Modifier.clearAndSetSemantics {},
        )
    }
}
