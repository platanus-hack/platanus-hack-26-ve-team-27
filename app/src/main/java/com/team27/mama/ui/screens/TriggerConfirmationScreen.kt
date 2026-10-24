package com.team27.mama.ui.screens

import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.padding
import androidx.compose.material3.Card
import androidx.compose.material3.CardDefaults
import androidx.compose.material3.ExperimentalMaterial3Api
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.OutlinedButton
import androidx.compose.material3.Scaffold
import androidx.compose.material3.Text
import androidx.compose.material3.TopAppBar
import androidx.compose.runtime.Composable
import androidx.compose.ui.Modifier
import androidx.compose.ui.tooling.preview.Preview
import androidx.compose.ui.unit.dp
import com.team27.mama.ui.theme.MamaTheme

/** Placeholder for the trigger confirmation screen. Ticket #8 designs the real one. */
@OptIn(ExperimentalMaterial3Api::class)
@Composable
fun TriggerConfirmationScreen(onBack: () -> Unit) {
    Scaffold(
        topBar = { TopAppBar(title = { Text("Alerta activada") }) },
    ) { padding ->
        Column(
            modifier = Modifier
                .fillMaxSize()
                .padding(padding)
                .padding(horizontal = 16.dp, vertical = 24.dp),
            verticalArrangement = Arrangement.spacedBy(16.dp),
        ) {
            Card(
                modifier = Modifier.fillMaxWidth(),
                colors = CardDefaults.cardColors(
                    containerColor = MaterialTheme.colorScheme.primaryContainer,
                    contentColor = MaterialTheme.colorScheme.onPrimaryContainer,
                ),
            ) {
                Column(
                    modifier = Modifier.padding(16.dp),
                    verticalArrangement = Arrangement.spacedBy(8.dp),
                ) {
                    Text(
                        text = "Mamá te va a llamar",
                        style = MaterialTheme.typography.headlineSmall,
                    )
                    Text(
                        text = "Pantalla provisional de confirmación.",
                        style = MaterialTheme.typography.bodyMedium,
                    )
                }
            }
            OutlinedButton(onClick = onBack, modifier = Modifier.fillMaxWidth()) {
                Text("Volver")
            }
        }
    }
}

@Preview(showBackground = true)
@Composable
private fun TriggerConfirmationScreenPreview() {
    MamaTheme { TriggerConfirmationScreen(onBack = {}) }
}
