package com.team27.mama.ui.screens

import android.content.res.Configuration
import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.rememberScrollState
import androidx.compose.foundation.verticalScroll
import androidx.compose.material3.Card
import androidx.compose.material3.CardDefaults
import androidx.compose.material3.CenterAlignedTopAppBar
import androidx.compose.material3.ExperimentalMaterial3Api
import androidx.compose.material3.HorizontalDivider
import androidx.compose.material3.ListItem
import androidx.compose.material3.ListItemDefaults
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.Scaffold
import androidx.compose.material3.Text
import androidx.compose.material3.TextButton
import androidx.compose.runtime.Composable
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.remember
import androidx.compose.runtime.setValue
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.platform.LocalContext
import androidx.compose.ui.semantics.contentDescription
import androidx.compose.ui.semantics.semantics
import androidx.compose.ui.tooling.preview.Preview
import androidx.compose.ui.unit.dp
import com.team27.mama.profile.Profile
import com.team27.mama.setup.SetupStore
import com.team27.mama.setup.SharedPreferencesStore
import com.team27.mama.ui.components.MamaCharacter
import com.team27.mama.ui.components.MamaPose
import com.team27.mama.ui.theme.MamaSpacing
import com.team27.mama.ui.theme.MamaTheme
import com.team27.mama.ui.theme.MamaWordmark

/*
 * Read-only profile (#85), opened from the trigger screen: the name, the
 * numbers (masked) and the two phrases (hidden until "Ver"). Nothing here
 * edits; setup (#15, #108) owns the data. It is one tap away from the trigger
 * screen, so it keeps that screen's bystander rule: neutral labels, the
 * duress phrase is "Frase clave" and never explained.
 */

private const val NOT_SET = "Sin configurar"
private const val HIDDEN = "••••••••"

@Composable
fun ProfileScreen(onBack: () -> Unit) {
    val context = LocalContext.current
    val profile = remember { Profile.from(SetupStore(SharedPreferencesStore.forSetup(context))) }
    ProfileContent(profile = profile, onBack = onBack)
}

@OptIn(ExperimentalMaterial3Api::class)
@Composable
private fun ProfileContent(profile: Profile, onBack: () -> Unit) {
    Scaffold(
        topBar = { CenterAlignedTopAppBar(title = { MamaWordmark(compact = true) }) },
    ) { padding ->
        Column(
            modifier = Modifier
                .fillMaxSize()
                .padding(padding)
                .verticalScroll(rememberScrollState())
                .padding(horizontal = MamaSpacing.screen, vertical = MamaSpacing.l),
            verticalArrangement = Arrangement.spacedBy(MamaSpacing.m),
        ) {
            Column(verticalArrangement = Arrangement.spacedBy(MamaSpacing.s)) {
                MamaCharacter(pose = MamaPose.Idle, size = 128.dp, modifier = Modifier.align(Alignment.CenterHorizontally))
                Text(
                    text = profile.name?.let { "Hola, $it" } ?: "Tu perfil",
                    style = MaterialTheme.typography.headlineMedium,
                    color = MaterialTheme.colorScheme.onBackground,
                )
                Text(
                    text = "Así te conoce Mamá.",
                    style = MaterialTheme.typography.bodyLarge,
                    color = MaterialTheme.colorScheme.onSurfaceVariant,
                )
            }

            SectionLabel("Tú")
            GroupCard {
                ValueRow(label = "Nombre", value = profile.name ?: NOT_SET)
                HorizontalDivider(color = MaterialTheme.colorScheme.outlineVariant)
                ValueRow(label = "Tu número", value = profile.ownNumber ?: NOT_SET)
            }

            SectionLabel("Tu familia")
            GroupCard {
                if (profile.familyNumbers.isEmpty()) {
                    ValueRow(label = "Números", value = NOT_SET)
                }
                profile.familyNumbers.forEachIndexed { index, number ->
                    if (index > 0) HorizontalDivider(color = MaterialTheme.colorScheme.outlineVariant)
                    ValueRow(label = "Familiar ${index + 1}", value = number)
                }
            }

            SectionLabel("Tus frases")
            GroupCard {
                PhraseRow(label = "Frase clave", phrase = profile.alertPhrase)
                HorizontalDivider(color = MaterialTheme.colorScheme.outlineVariant)
                PhraseRow(label = "Frase de todo bien", phrase = profile.safePhrase)
            }

            TextButton(onClick = onBack, modifier = Modifier.align(Alignment.CenterHorizontally)) {
                Text("Volver")
            }
        }
    }
}

@Composable
private fun SectionLabel(text: String) {
    Text(text = text, style = MaterialTheme.typography.titleSmall, color = MaterialTheme.colorScheme.primary)
}

@Composable
private fun GroupCard(content: @Composable () -> Unit) {
    Card(
        modifier = Modifier.fillMaxWidth(),
        colors = CardDefaults.cardColors(containerColor = MaterialTheme.colorScheme.surfaceContainerLow),
    ) {
        content()
    }
}

@Composable
private fun ValueRow(label: String, value: String) {
    ListItem(
        modifier = Modifier.semantics(mergeDescendants = true) {},
        overlineContent = { Text(label) },
        headlineContent = { Text(value) },
        colors = ListItemDefaults.colors(containerColor = MaterialTheme.colorScheme.surfaceContainerLow),
    )
}

/**
 * A phrase stays hidden until "Ver"; an unset phrase has nothing to reveal.
 * Plain remember, not saveable: a rotation or a restore hides it again.
 */
@Composable
private fun PhraseRow(label: String, phrase: String?) {
    var shown by remember { mutableStateOf(false) }
    ListItem(
        overlineContent = { Text(label) },
        headlineContent = {
            Text(
                when {
                    phrase == null -> NOT_SET
                    shown -> phrase
                    else -> HIDDEN
                },
                modifier = if (phrase != null && !shown) Modifier.semantics { contentDescription = "Oculta" } else Modifier,
            )
        },
        trailingContent = if (phrase == null) {
            null
        } else {
            {
                val action = if (shown) "Ocultar" else "Ver"
                TextButton(
                    onClick = { shown = !shown },
                    // Two "Ver" buttons sound the same to TalkBack; name the phrase.
                    modifier = Modifier.semantics { contentDescription = "$action ${label.lowercase()}" },
                ) { Text(action) }
            }
        },
        colors = ListItemDefaults.colors(containerColor = MaterialTheme.colorScheme.surfaceContainerLow),
    )
}

private val previewProfile = Profile(
    name = "Mariana",
    ownNumber = "•••• 4567",
    familyNumbers = listOf("•••• 2233", "•••• 8877"),
    alertPhrase = "dile a la tía Carmen que llego tarde",
    safePhrase = null,
)

@Preview(showBackground = true, heightDp = 900)
@Composable
private fun ProfileScreenPreview() {
    MamaTheme { ProfileContent(profile = previewProfile, onBack = {}) }
}

@Preview(showBackground = true, heightDp = 900, uiMode = Configuration.UI_MODE_NIGHT_YES)
@Composable
private fun ProfileScreenDarkPreview() {
    MamaTheme { ProfileContent(profile = previewProfile, onBack = {}) }
}
