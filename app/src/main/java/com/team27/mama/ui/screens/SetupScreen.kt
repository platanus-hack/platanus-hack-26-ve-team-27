package com.team27.mama.ui.screens

import android.content.res.Configuration
import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Box
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.PaddingValues
import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.navigationBarsPadding
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.layout.size
import androidx.compose.foundation.lazy.LazyColumn
import androidx.compose.foundation.shape.CircleShape
import androidx.compose.material3.Button
import androidx.compose.material3.Card
import androidx.compose.material3.CardDefaults
import androidx.compose.material3.CenterAlignedTopAppBar
import androidx.compose.material3.ExperimentalMaterial3Api
import androidx.compose.material3.HorizontalDivider
import androidx.compose.material3.Icon
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
import androidx.compose.ui.tooling.preview.Preview
import androidx.compose.ui.unit.dp
import com.team27.mama.R
import com.team27.mama.ui.theme.MamaSpacing
import com.team27.mama.ui.theme.MamaTheme
import com.team27.mama.ui.theme.MamaWordmark

/*
 * Reference screen for first-run setup (#8). Ticket #15 replaces the static
 * data below with the real permission flow, family numbers and duress phrase,
 * keeping this layout: wordmark, short pitch, numbered steps, one primary CTA.
 */

/** One permission step. [done] is placeholder state until #15 wires the real checks. */
private data class SetupStep(val title: String, val why: String, val done: Boolean)

// Order is the product order (docs/MAMA-summary.md): accessibility always last.
private val permissionSteps = listOf(
    SetupStep("Notificaciones", "Para avisarte que tu familia ya sabe.", done = true),
    SetupStep("Ubicación", "Primero mientras usas la app, luego siempre.", done = true),
    SetupStep("Micrófono", "Para escuchar si algo va mal.", done = false),
    SetupStep("Contactos", "Para guardar a Mamá en tu teléfono.", done = false),
    SetupStep("SMS", "Respaldo cuando no hay datos.", done = false),
    SetupStep("Batería sin límites", "Para que el teléfono no apague a MAMÁ.", done = false),
    SetupStep("Accesibilidad", "Para notar subir-bajar-subir con el teléfono bloqueado.", done = false),
)

private val familyNumbers = listOf(
    "Papá" to "+58 412 555 0101",
    "Mi hermana" to "+58 414 555 0123",
)

@OptIn(ExperimentalMaterial3Api::class)
@Composable
fun SetupScreen(onContinue: () -> Unit) {
    Scaffold(
        topBar = { CenterAlignedTopAppBar(title = { MamaWordmark(compact = true) }) },
        bottomBar = {
            Surface(color = MaterialTheme.colorScheme.surface) {
                Button(
                    onClick = onContinue,
                    modifier = Modifier
                        .fillMaxWidth()
                        .navigationBarsPadding()
                        .padding(horizontal = MamaSpacing.screen, vertical = MamaSpacing.m),
                    contentPadding = PaddingValues(vertical = MamaSpacing.m),
                ) {
                    Text("Empezar", style = MaterialTheme.typography.titleMedium)
                }
            }
        },
    ) { padding ->
        LazyColumn(
            modifier = Modifier
                .fillMaxSize()
                .padding(padding),
            contentPadding = PaddingValues(horizontal = MamaSpacing.screen, vertical = MamaSpacing.l),
            verticalArrangement = Arrangement.spacedBy(MamaSpacing.m),
        ) {
            item {
                Column(verticalArrangement = Arrangement.spacedBy(MamaSpacing.s)) {
                    Text(
                        text = "Tu familia, a un gesto",
                        style = MaterialTheme.typography.headlineMedium,
                        color = MaterialTheme.colorScheme.onBackground,
                    )
                    Text(
                        text = "Si algún día estás en peligro, presionas subir-bajar-subir y Mamá te llama. " +
                            "Quien esté a tu lado solo ve una llamada normal.",
                        style = MaterialTheme.typography.bodyLarge,
                        color = MaterialTheme.colorScheme.onSurfaceVariant,
                    )
                }
            }

            item { SectionLabel("Permisos, en este orden") }
            item {
                Card(
                    colors = CardDefaults.cardColors(containerColor = MaterialTheme.colorScheme.surfaceContainerLow),
                ) {
                    permissionSteps.forEachIndexed { index, step ->
                        if (index > 0) HorizontalDivider(color = MaterialTheme.colorScheme.outlineVariant)
                        StepRow(number = index + 1, step = step)
                    }
                }
            }

            item { SectionLabel("Tu familia") }
            item {
                Card(
                    colors = CardDefaults.cardColors(containerColor = MaterialTheme.colorScheme.surfaceContainerLow),
                ) {
                    familyNumbers.forEach { (name, number) ->
                        ListItem(
                            headlineContent = { Text(name) },
                            supportingContent = { Text(number) },
                            colors = ListItemDefaults.colors(containerColor = MaterialTheme.colorScheme.surfaceContainerLow),
                        )
                    }
                    TextButton(
                        onClick = {},
                        modifier = Modifier.padding(start = MamaSpacing.s, bottom = MamaSpacing.s),
                    ) { Text("Agregar otro número") }
                }
            }

            item { SectionLabel("Tu frase secreta") }
            item {
                Card(
                    colors = CardDefaults.cardColors(
                        containerColor = MaterialTheme.colorScheme.tertiaryContainer,
                        contentColor = MaterialTheme.colorScheme.onTertiaryContainer,
                    ),
                ) {
                    Column(
                        modifier = Modifier.padding(MamaSpacing.m),
                        verticalArrangement = Arrangement.spacedBy(MamaSpacing.s),
                    ) {
                        Text(
                            text = "«Dile a la tía Carmen que llego tarde»",
                            style = MaterialTheme.typography.titleLarge,
                        )
                        Text(
                            text = "Si la dices en la llamada, tu familia recibe dónde estás. " +
                                "Para quien te escucha, es una frase cualquiera.",
                            style = MaterialTheme.typography.bodyMedium,
                        )
                    }
                }
            }

            item { SectionLabel("El contacto Mamá") }
            item {
                Card(
                    colors = CardDefaults.cardColors(
                        containerColor = MaterialTheme.colorScheme.primaryContainer,
                        contentColor = MaterialTheme.colorScheme.onPrimaryContainer,
                    ),
                ) {
                    ListItem(
                        headlineContent = { Text("Mamá") },
                        supportingContent = { Text("La guardamos en tus contactos para que su llamada se vea normal.") },
                        leadingContent = { Initial("M") },
                        colors = ListItemDefaults.colors(
                            containerColor = MaterialTheme.colorScheme.primaryContainer,
                            headlineColor = MaterialTheme.colorScheme.onPrimaryContainer,
                            supportingColor = MaterialTheme.colorScheme.onPrimaryContainer,
                        ),
                    )
                }
            }
        }
    }
}

@Composable
private fun SectionLabel(text: String) {
    Text(
        text = text,
        modifier = Modifier.padding(top = MamaSpacing.s),
        style = MaterialTheme.typography.titleSmall,
        color = MaterialTheme.colorScheme.primary,
    )
}

@Composable
private fun StepRow(number: Int, step: SetupStep) {
    ListItem(
        headlineContent = { Text(step.title) },
        supportingContent = { Text(step.why) },
        leadingContent = {
            Surface(
                modifier = Modifier.size(32.dp),
                shape = CircleShape,
                color = if (step.done) MaterialTheme.colorScheme.primary else MaterialTheme.colorScheme.surfaceContainerHighest,
                contentColor = if (step.done) MaterialTheme.colorScheme.onPrimary else MaterialTheme.colorScheme.onSurfaceVariant,
            ) {
                Box(contentAlignment = Alignment.Center) {
                    if (step.done) {
                        Icon(
                            painter = painterResource(R.drawable.ic_check),
                            contentDescription = "Listo",
                            modifier = Modifier.size(20.dp),
                        )
                    } else {
                        Text(number.toString(), style = MaterialTheme.typography.labelLarge)
                    }
                }
            }
        },
        colors = ListItemDefaults.colors(containerColor = MaterialTheme.colorScheme.surfaceContainerLow),
    )
}

@Composable
private fun Initial(letter: String) {
    Surface(
        modifier = Modifier.size(40.dp),
        shape = CircleShape,
        color = MaterialTheme.colorScheme.primary,
        contentColor = MaterialTheme.colorScheme.onPrimary,
    ) {
        Box(contentAlignment = Alignment.Center) {
            Text(letter, style = MaterialTheme.typography.titleMedium)
        }
    }
}

@Preview(showBackground = true, heightDp = 1600)
@Composable
private fun SetupScreenPreview() {
    MamaTheme { SetupScreen(onContinue = {}) }
}

@Preview(showBackground = true, heightDp = 1600, uiMode = Configuration.UI_MODE_NIGHT_YES)
@Composable
private fun SetupScreenDarkPreview() {
    MamaTheme { SetupScreen(onContinue = {}) }
}
