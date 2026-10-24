package com.team27.mama.ui.screens

import android.content.res.Configuration
import androidx.compose.foundation.BorderStroke
import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Box
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.layout.size
import androidx.compose.foundation.rememberScrollState
import androidx.compose.foundation.shape.CircleShape
import androidx.compose.foundation.verticalScroll
import androidx.compose.material3.Card
import androidx.compose.material3.CardDefaults
import androidx.compose.material3.CenterAlignedTopAppBar
import androidx.compose.material3.CircularProgressIndicator
import androidx.compose.material3.ExperimentalMaterial3Api
import androidx.compose.material3.HorizontalDivider
import androidx.compose.material3.Icon
import androidx.compose.material3.LinearProgressIndicator
import androidx.compose.material3.ListItem
import androidx.compose.material3.ListItemDefaults
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.Scaffold
import androidx.compose.material3.Surface
import androidx.compose.material3.Text
import androidx.compose.material3.TextButton
import androidx.compose.runtime.Composable
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.res.painterResource
import androidx.compose.ui.semantics.semantics
import androidx.compose.ui.semantics.stateDescription
import androidx.compose.ui.text.style.TextAlign
import androidx.compose.ui.tooling.preview.Preview
import androidx.compose.ui.unit.dp
import com.team27.mama.R
import com.team27.mama.emergency.DeliveryBadge
import com.team27.mama.emergency.EmergencyPhase
import com.team27.mama.emergency.EmergencyState
import com.team27.mama.emergency.EmergencyStatus
import com.team27.mama.emergency.MeshIssue
import com.team27.mama.emergency.toStatus
import com.team27.mama.ui.theme.MamaSpacing
import com.team27.mama.ui.theme.MamaTheme
import com.team27.mama.ui.theme.MamaWordmark

/*
 * Reference screen for what the user sees right after the trigger (#8):
 * volume up-down-up or the widget. A bystander may read it, so it must look
 * like "my mom is about to call": no red outside a failure, and no words like
 * alert, emergency, danger or duress phrase. Never explain on this screen what
 * the family gets or how the phrase works.
 * #16 shows it from the trigger, #24 drives the step states (sent, calling,
 * notified, failed) and #38 replaces the elapsed placeholder with a real
 * counter. The counter and the steps are there for the live demo (#38); keep
 * their labels neutral. Layout: message, elapsed area, steps, discreet cancel.
 */

/**
 * State of one step, with what TalkBack announces. [Failed] is the only place
 * red appears: an errorContainer row with an error badge.
 */
private enum class StepState(val spoken: String) {
    Done("listo"),
    Running("en curso"),
    Pending("pendiente"),
    Failed("no se pudo"),
}

private data class TriggerStep(
    val label: String,
    val state: StepState,
    val time: String?,
    val detail: String? = null,
)

private fun EmergencyStatus.toStep() = TriggerStep(
    label = text,
    state = when (badge) {
        DeliveryBadge.RUNNING -> StepState.Running
        DeliveryBadge.DONE -> StepState.Done
        DeliveryBadge.FAILED -> StepState.Failed
    },
    time = null,
    detail = detail,
)

// Placeholder data until #24 / #38 feed the real incident timestamps.
private val placeholderSteps = listOf(
    TriggerStep("Enviado", StepState.Done, "0,8 s"),
    TriggerStep("Llamada de Mamá", StepState.Running, "…"),
    TriggerStep("Mensaje a casa", StepState.Pending, "—"),
)

@OptIn(ExperimentalMaterial3Api::class)
@Composable
fun TriggerConfirmationScreen(onBack: () -> Unit, status: EmergencyStatus? = null) {
    val steps = if (status == null) placeholderSteps else listOf(status.toStep()) + placeholderSteps.drop(1)
    Scaffold(
        topBar = { CenterAlignedTopAppBar(title = { MamaWordmark(compact = true) }) },
    ) { padding ->
        Column(
            modifier = Modifier
                .fillMaxSize()
                .padding(padding)
                .verticalScroll(rememberScrollState())
                .padding(horizontal = MamaSpacing.screen, vertical = MamaSpacing.l),
            verticalArrangement = Arrangement.spacedBy(MamaSpacing.l),
        ) {
            Column(verticalArrangement = Arrangement.spacedBy(MamaSpacing.s)) {
                Text(
                    text = "Mamá te va a llamar",
                    style = MaterialTheme.typography.displaySmall,
                    color = MaterialTheme.colorScheme.onBackground,
                )
                Text(
                    text = "Contesta como siempre y habla normal.",
                    style = MaterialTheme.typography.bodyLarge,
                    color = MaterialTheme.colorScheme.onSurfaceVariant,
                )
            }

            LinearProgressIndicator(
                modifier = Modifier.fillMaxWidth(),
                color = MaterialTheme.colorScheme.primary,
                trackColor = MaterialTheme.colorScheme.primaryContainer,
            )

            ElapsedCard(elapsed = "00:04")

            Card(colors = CardDefaults.cardColors(containerColor = MaterialTheme.colorScheme.surfaceContainerLow)) {
                steps.forEachIndexed { index, step ->
                    if (index > 0) HorizontalDivider(color = MaterialTheme.colorScheme.outlineVariant)
                    StepRow(step)
                }
            }

            Column(
                modifier = Modifier.fillMaxWidth(),
                horizontalAlignment = Alignment.CenterHorizontally,
            ) {
                TextButton(onClick = onBack) {
                    Text("Fue sin querer")
                }
                Text(
                    text = "Tócalo solo si fue un error.",
                    style = MaterialTheme.typography.bodySmall,
                    color = MaterialTheme.colorScheme.onSurfaceVariant,
                    textAlign = TextAlign.Center,
                )
            }
        }
    }
}

/** Big, projector-readable elapsed time. #38 feeds the real value. */
@Composable
private fun ElapsedCard(elapsed: String) {
    Card(
        modifier = Modifier.fillMaxWidth(),
        colors = CardDefaults.cardColors(
            containerColor = MaterialTheme.colorScheme.primaryContainer,
            contentColor = MaterialTheme.colorScheme.onPrimaryContainer,
        ),
    ) {
        Column(
            modifier = Modifier
                .fillMaxWidth()
                .padding(vertical = MamaSpacing.l, horizontal = MamaSpacing.m),
            horizontalAlignment = Alignment.CenterHorizontally,
            verticalArrangement = Arrangement.spacedBy(MamaSpacing.xs),
        ) {
            Text(text = elapsed, style = MaterialTheme.typography.displayLarge)
            Text(text = "desde que empezó", style = MaterialTheme.typography.labelLarge)
        }
    }
}

@Composable
private fun StepRow(step: TriggerStep) {
    val failed = step.state == StepState.Failed
    ListItem(
        modifier = Modifier.semantics(mergeDescendants = true) { stateDescription = step.state.spoken },
        headlineContent = { Text(step.label) },
        leadingContent = { StepBadge(step.state) },
        supportingContent = step.detail?.let { detail ->
            { Text(detail, style = MaterialTheme.typography.bodyMedium) }
        },
        trailingContent = step.time?.let { time ->
            { Text(time, style = MaterialTheme.typography.labelLarge) }
        },
        colors = ListItemDefaults.colors(
            containerColor = if (failed) MaterialTheme.colorScheme.errorContainer else MaterialTheme.colorScheme.surfaceContainerLow,
            supportingColor = if (failed) MaterialTheme.colorScheme.onErrorContainer else MaterialTheme.colorScheme.onSurfaceVariant,
            headlineColor = if (failed) MaterialTheme.colorScheme.onErrorContainer else MaterialTheme.colorScheme.onSurface,
            trailingIconColor = if (failed) MaterialTheme.colorScheme.onErrorContainer else MaterialTheme.colorScheme.onSurfaceVariant,
        ),
    )
}

@Composable
private fun StepBadge(state: StepState) {
    val badge = Modifier.size(32.dp)
    when (state) {
        StepState.Done -> Surface(
            modifier = badge,
            shape = CircleShape,
            color = MaterialTheme.colorScheme.primary,
            contentColor = MaterialTheme.colorScheme.onPrimary,
        ) {
            Box(contentAlignment = Alignment.Center) {
                Icon(painterResource(R.drawable.ic_check), contentDescription = null, modifier = Modifier.size(20.dp))
            }
        }
        StepState.Running -> CircularProgressIndicator(
            modifier = badge.padding(MamaSpacing.xs),
            color = MaterialTheme.colorScheme.primary,
            trackColor = MaterialTheme.colorScheme.primaryContainer,
            strokeWidth = 4.dp,
        )
        StepState.Pending -> Surface(
            modifier = badge,
            shape = CircleShape,
            color = MaterialTheme.colorScheme.surfaceContainerHighest,
            border = BorderStroke(2.dp, MaterialTheme.colorScheme.outline),
        ) {}
        StepState.Failed -> Surface(
            modifier = badge,
            shape = CircleShape,
            color = MaterialTheme.colorScheme.error,
            contentColor = MaterialTheme.colorScheme.onError,
        ) {
            Box(contentAlignment = Alignment.Center) {
                Text("!", style = MaterialTheme.typography.labelLarge)
            }
        }
    }
}

@Preview(showBackground = true, heightDp = 900)
@Composable
private fun TriggerConfirmationScreenPreview() {
    MamaTheme { TriggerConfirmationScreen(onBack = {}) }
}

@Preview(showBackground = true, heightDp = 900, uiMode = Configuration.UI_MODE_NIGHT_YES)
@Composable
private fun TriggerConfirmationScreenDarkPreview() {
    MamaTheme { TriggerConfirmationScreen(onBack = {}) }
}

@Preview(showBackground = true, heightDp = 900)
@Composable
private fun TriggerConfirmationSearchingPreview() {
    MamaTheme {
        TriggerConfirmationScreen(
            onBack = {},
            status = EmergencyState(
                phase = EmergencyPhase.SEARCHING_NEARBY,
                meshIssue = MeshIssue.UNAVAILABLE,
            ).toStatus(),
        )
    }
}

@Preview(showBackground = true, heightDp = 900)
@Composable
private fun TriggerConfirmationNotifiedPreview() {
    MamaTheme {
        TriggerConfirmationScreen(
            onBack = {},
            status = EmergencyState(phase = EmergencyPhase.NOTIFIED).toStatus(),
        )
    }
}
