package com.team27.mama.ui.screens

import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.WindowInsets
import androidx.compose.foundation.layout.WindowInsetsSides
import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.only
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.layout.safeDrawing
import androidx.compose.foundation.layout.windowInsetsPadding
import androidx.compose.foundation.rememberScrollState
import androidx.compose.foundation.verticalScroll
import androidx.compose.material3.Card
import androidx.compose.material3.CardDefaults
import androidx.compose.material3.CenterAlignedTopAppBar
import androidx.compose.material3.ExperimentalMaterial3Api
import androidx.compose.material3.ListItem
import androidx.compose.material3.ListItemDefaults
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.Scaffold
import androidx.compose.material3.Switch
import androidx.compose.material3.Text
import androidx.compose.material3.TextButton
import androidx.compose.runtime.Composable
import androidx.compose.ui.Modifier
import androidx.compose.ui.tooling.preview.Preview
import com.team27.mama.relay.relayedCountText
import com.team27.mama.ui.theme.MamaSpacing
import com.team27.mama.ui.theme.MamaTheme
import com.team27.mama.ui.theme.MamaWordmark

/** On/off switch (on by default) for relaying nearby alerts, plus how many this phone has relayed. */
@OptIn(ExperimentalMaterial3Api::class)
@Composable
fun SettingsScreen(
    enabled: Boolean,
    relayedCount: Int,
    permissionMissing: Boolean,
    onEnabledChange: (Boolean) -> Unit,
    onBack: () -> Unit,
) {
    Scaffold(
        topBar = { CenterAlignedTopAppBar(title = { MamaWordmark(compact = true) }) },
        bottomBar = {
            TextButton(
                onClick = onBack,
                modifier = Modifier
                    .fillMaxWidth()
                    .windowInsetsPadding(WindowInsets.safeDrawing.only(WindowInsetsSides.Bottom))
                    .padding(horizontal = MamaSpacing.screen, vertical = MamaSpacing.s),
            ) { Text("Volver") }
        },
    ) { padding ->
        Column(
            modifier = Modifier
                .fillMaxSize()
                .padding(padding)
                .verticalScroll(rememberScrollState())
                .padding(horizontal = MamaSpacing.screen, vertical = MamaSpacing.l),
            verticalArrangement = Arrangement.spacedBy(MamaSpacing.m),
        ) {
            Text("Ayuda cercana", style = MaterialTheme.typography.headlineMedium)
            Text(
                "Si alguien cerca de ti con MAMÁ pide ayuda sin internet, tu teléfono puede pasar su alerta.",
                style = MaterialTheme.typography.bodyLarge,
                color = MaterialTheme.colorScheme.onSurfaceVariant,
            )
            Card(
                modifier = Modifier.fillMaxWidth(),
                colors = CardDefaults.cardColors(containerColor = MaterialTheme.colorScheme.surfaceContainerLow),
            ) {
                ListItem(
                    headlineContent = { Text("Ayudar a otros cerca de ti") },
                    supportingContent = {
                        Text("Funciona por Bluetooth, también con MAMÁ cerrada. No ves su ubicación ni sus datos.")
                    },
                    trailingContent = { Switch(checked = enabled, onCheckedChange = onEnabledChange) },
                    colors = ListItemDefaults.colors(containerColor = MaterialTheme.colorScheme.surfaceContainerLow),
                )
            }
            Text(
                relayedCountText(relayedCount),
                style = MaterialTheme.typography.bodyMedium,
                color = MaterialTheme.colorScheme.onSurfaceVariant,
            )
            Text(
                "Si reinicias el teléfono, abre MAMÁ una vez para volver a activarlo.",
                style = MaterialTheme.typography.bodyMedium,
                color = MaterialTheme.colorScheme.onSurfaceVariant,
            )
            if (permissionMissing) {
                Card(
                    modifier = Modifier.fillMaxWidth(),
                    colors = CardDefaults.cardColors(
                        containerColor = MaterialTheme.colorScheme.errorContainer,
                        contentColor = MaterialTheme.colorScheme.onErrorContainer,
                    ),
                ) {
                    Text(
                        "Para ayudar a otros, MAMÁ necesita permiso de Bluetooth y ubicación.",
                        style = MaterialTheme.typography.bodyMedium,
                        modifier = Modifier.padding(MamaSpacing.m),
                    )
                }
            }
        }
    }
}

@Preview(showBackground = true)
@Composable
private fun SettingsOffPreview() {
    MamaTheme { SettingsScreen(false, 0, false, {}, {}) }
}

@Preview(showBackground = true)
@Composable
private fun SettingsOnPreview() {
    MamaTheme { SettingsScreen(true, 3, true, {}, {}) }
}
