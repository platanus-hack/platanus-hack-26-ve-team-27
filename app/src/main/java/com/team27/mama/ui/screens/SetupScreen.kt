package com.team27.mama.ui.screens

import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.padding
import androidx.compose.material3.Button
import androidx.compose.material3.Card
import androidx.compose.material3.ExperimentalMaterial3Api
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.Scaffold
import androidx.compose.material3.Text
import androidx.compose.material3.TopAppBar
import androidx.compose.runtime.Composable
import androidx.compose.ui.Modifier
import androidx.compose.ui.tooling.preview.Preview
import androidx.compose.ui.unit.dp
import com.team27.mama.ui.theme.MamaTheme

/** Placeholder for the first-run setup screen. Ticket #8 designs the real one. */
@OptIn(ExperimentalMaterial3Api::class)
@Composable
fun SetupScreen(onContinue: () -> Unit) {
    Scaffold(
        topBar = { TopAppBar(title = { Text("Configuración") }) },
    ) { padding ->
        Column(
            modifier = Modifier
                .fillMaxSize()
                .padding(padding)
                .padding(horizontal = 16.dp, vertical = 24.dp),
            verticalArrangement = Arrangement.spacedBy(16.dp),
        ) {
            Text(
                text = "Bienvenida a MAMÁ",
                style = MaterialTheme.typography.headlineMedium,
                color = MaterialTheme.colorScheme.onBackground,
            )
            Text(
                text = "Aquí vamos a pedir los permisos y guardar los números de tu familia.",
                style = MaterialTheme.typography.bodyLarge,
                color = MaterialTheme.colorScheme.onSurfaceVariant,
            )
            Card(modifier = Modifier.fillMaxWidth()) {
                Text(
                    text = "Pantalla provisional",
                    modifier = Modifier.padding(16.dp),
                    style = MaterialTheme.typography.titleMedium,
                )
            }
            Button(onClick = onContinue, modifier = Modifier.fillMaxWidth()) {
                Text("Continuar")
            }
        }
    }
}

@Preview(showBackground = true)
@Composable
private fun SetupScreenPreview() {
    MamaTheme { SetupScreen(onContinue = {}) }
}
