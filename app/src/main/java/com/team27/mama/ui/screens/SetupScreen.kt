package com.team27.mama.ui.screens

import android.content.res.Configuration
import androidx.activity.compose.LocalActivity
import androidx.activity.compose.rememberLauncherForActivityResult
import androidx.activity.result.contract.ActivityResultContracts
import androidx.compose.foundation.BorderStroke
import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Box
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.PaddingValues
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.WindowInsets
import androidx.compose.foundation.layout.WindowInsetsSides
import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.only
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.layout.safeDrawing
import androidx.compose.foundation.layout.size
import androidx.compose.foundation.layout.windowInsetsPadding
import androidx.compose.foundation.rememberScrollState
import androidx.compose.foundation.shape.CircleShape
import androidx.compose.foundation.text.KeyboardOptions
import androidx.compose.foundation.verticalScroll
import androidx.compose.material3.Button
import androidx.compose.material3.Card
import androidx.compose.material3.CardDefaults
import androidx.compose.material3.CenterAlignedTopAppBar
import androidx.compose.material3.CircularProgressIndicator
import androidx.compose.material3.ExperimentalMaterial3Api
import androidx.compose.material3.HorizontalDivider
import androidx.compose.material3.Icon
import androidx.compose.material3.ListItem
import androidx.compose.material3.ListItemDefaults
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.OutlinedButton
import androidx.compose.material3.OutlinedTextField
import androidx.compose.material3.Scaffold
import androidx.compose.material3.Surface
import androidx.compose.material3.Text
import androidx.compose.material3.TextButton
import androidx.compose.runtime.Composable
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.saveable.rememberSaveable
import androidx.compose.runtime.setValue
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.graphics.Color
import androidx.compose.ui.platform.LocalContext
import androidx.compose.ui.res.painterResource
import androidx.compose.ui.semantics.semantics
import androidx.compose.ui.semantics.stateDescription
import androidx.compose.ui.text.input.ImeAction
import androidx.compose.ui.text.input.KeyboardCapitalization
import androidx.compose.ui.text.input.KeyboardType
import androidx.compose.ui.tooling.preview.Preview
import androidx.compose.ui.unit.dp
import androidx.core.app.ActivityCompat
import androidx.lifecycle.compose.LifecycleResumeEffect
import androidx.lifecycle.compose.collectAsStateWithLifecycle
import androidx.lifecycle.viewmodel.compose.viewModel
import com.team27.mama.BuildConfig
import com.team27.mama.R
import com.team27.mama.setup.MAMA_CONTACT_NAME
import com.team27.mama.setup.PermissionFlow
import com.team27.mama.setup.PhoneNumbers
import com.team27.mama.setup.SetupForm
import com.team27.mama.setup.SetupPhase
import com.team27.mama.setup.SetupStep
import com.team27.mama.setup.SetupTask
import com.team27.mama.setup.SetupUiState
import com.team27.mama.setup.SetupViewModel
import com.team27.mama.setup.StepStatus
import com.team27.mama.setup.SystemScreens
import com.team27.mama.setup.TaskStatus
import com.team27.mama.setup.openFirst
import com.team27.mama.ui.theme.MamaSpacing
import com.team27.mama.ui.theme.MamaTheme
import com.team27.mama.ui.theme.MamaWordmark

/*
 * First-run setup (#15), on the #8 reference layout: wordmark, one sentence,
 * numbered steps, one primary CTA. Four phases: permissions in order, the
 * form, saving "Mamá" and registering, done. A finished setup opens on done.
 */

/** Spanish copy for one permission step. [hint] shows only while it is the current step. */
private data class StepCopy(
    val title: String,
    val why: String,
    val action: String,
    val denied: String = "",
    val hint: String? = null,
)

private fun copyFor(step: SetupStep): StepCopy = when (step) {
    SetupStep.NOTIFICATIONS -> StepCopy(
        title = "Notificaciones",
        why = "Para avisarte que tu familia ya sabe.",
        action = "Permitir notificaciones",
        denied = "Sin notificaciones no podemos confirmarte que tu familia recibió el aviso.",
    )
    SetupStep.LOCATION -> StepCopy(
        title = "Ubicación",
        why = "Primero mientras usas la app.",
        action = "Permitir ubicación",
        denied = "Sin ubicación tu familia no sabrá dónde estás.",
    )
    SetupStep.BACKGROUND_LOCATION -> StepCopy(
        title = "Ubicación todo el tiempo",
        why = "Para enviar dónde estás aunque el teléfono esté bloqueado.",
        action = "Permitir todo el tiempo",
        denied = "Sin «Permitir todo el tiempo» no podemos enviar tu ubicación con el teléfono bloqueado.",
        hint = "En la pantalla que se abre, elige «Permitir todo el tiempo».",
    )
    SetupStep.MICROPHONE -> StepCopy(
        title = "Micrófono",
        why = "Para escuchar si algo va mal.",
        action = "Permitir micrófono",
        denied = "Sin micrófono no podemos guardar el audio de lo que pasa a tu alrededor.",
    )
    SetupStep.CONTACTS -> StepCopy(
        title = "Contactos",
        why = "Para guardar a Mamá en tu teléfono.",
        action = "Permitir contactos",
        denied = "Sin contactos no podemos guardar a Mamá, y su llamada saldría como un número desconocido.",
    )
    SetupStep.SMS -> StepCopy(
        title = "SMS",
        why = "Respaldo cuando no hay datos.",
        action = "Permitir SMS",
        denied = "Sin SMS no hay respaldo cuando te quedas sin datos.",
    )
    SetupStep.AUTOSTART -> StepCopy(
        title = "Inicio automático",
        why = "Para que MAMÁ siga activa después de reiniciar.",
        action = "Abrir inicio automático",
        hint = "Activa el interruptor de MAMÁ y vuelve aquí.",
    )
    SetupStep.BATTERY -> StepCopy(
        title = "Batería sin límites",
        why = "Para que el teléfono no cierre MAMÁ.",
        action = "Abrir ajustes de batería",
        hint = "Elige «Sin restricciones» y vuelve aquí.",
    )
    SetupStep.POPUPS -> StepCopy(
        title = "Ventanas emergentes",
        why = "Para que la llamada de Mamá aparezca aunque estés en otra app.",
        action = "Abrir permisos de ventanas",
        // HyperOS 3 labels; the screen behind "Otros permisos" cannot be opened directly by an app.
        hint = "Toca «Otros permisos» y permite «Abrir nuevas ventanas mientras se ejecuta en segundo plano» " +
            "y «Mostrar en pantalla de bloqueo». Luego vuelve aquí.",
    )
    SetupStep.ACCESSIBILITY -> StepCopy(
        title = "Accesibilidad",
        why = "Para detectar los botones de volumen con el teléfono bloqueado.",
        action = "Abrir accesibilidad",
        hint = "Si MAMÁ aparece bloqueada: Ajustes > Apps > MAMÁ > ⋮ > Permitir ajustes restringidos.",
    )
}

/** Everything the content can ask for; the stateful [SetupScreen] wires it to the ViewModel. */
private class SetupActions(
    val onStepAction: (SetupStep) -> Unit = {},
    val onOpenAppSettings: () -> Unit = {},
    val onSkip: (SetupStep) -> Unit = {},
    val onContinueToForm: () -> Unit = {},
    val onFormChange: (SetupForm) -> Unit = {},
    val onSubmit: () -> Unit = {},
    val onRetry: () -> Unit = {},
    val onEdit: () -> Unit = {},
    val onReviewPermissions: () -> Unit = {},
)

@Composable
fun SetupScreen(viewModel: SetupViewModel = viewModel()) {
    val state by viewModel.state.collectAsStateWithLifecycle()
    val context = LocalContext.current
    val activity = LocalActivity.current
    var requestedStep by rememberSaveable { mutableStateOf<SetupStep?>(null) }

    val permissionLauncher = rememberLauncherForActivityResult(
        ActivityResultContracts.RequestMultiplePermissions(),
    ) { result ->
        val step = requestedStep ?: return@rememberLauncherForActivityResult
        requestedStep = null
        // Empty: the request was interrupted (dismissed, activity recreated). The step stays as it was.
        if (result.isEmpty()) return@rememberLauncherForActivityResult
        val granted = step.isGrantedBy { result[it] == true }
        val canAskAgain = activity == null ||
            step.permissions.any { ActivityCompat.shouldShowRequestPermissionRationale(activity, it) }
        viewModel.onPermissionResult(step, granted, canAskAgain)
    }

    // Back from Settings (or a permission dialog): re-read what is granted now.
    LifecycleResumeEffect(viewModel) {
        viewModel.onResume()
        onPauseOrDispose {}
    }

    val openAppSettings = { context.openFirst(listOf(SystemScreens.appSettings)) }
    val actions = SetupActions(
        onStepAction = { step ->
            when {
                step.isSystemScreen -> {
                    val targets = SystemScreens.targetsFor(
                        step = step,
                        appPackage = context.packageName,
                        appLabel = context.getString(R.string.app_name),
                        isXiaomi = SystemScreens.isXiaomi(),
                    )
                    if (context.openFirst(targets)) viewModel.onSystemScreenOpened(step) else viewModel.skip(step)
                }
                state.permissions.statusOf(step) == StepStatus.BLOCKED -> openAppSettings()
                else -> {
                    requestedStep = step
                    permissionLauncher.launch(step.permissions.toTypedArray())
                }
            }
        },
        onOpenAppSettings = { openAppSettings() },
        onSkip = viewModel::skip,
        onContinueToForm = viewModel::continueToForm,
        onFormChange = viewModel::onFormChange,
        onSubmit = viewModel::submitForm,
        onRetry = viewModel::retry,
        onEdit = viewModel::edit,
        onReviewPermissions = viewModel::reviewPermissions,
    )
    val mamaNumber = PhoneNumbers.normalize(BuildConfig.TWILIO_NUMBER)?.let(PhoneNumbers::display)
    SetupContent(state = state, mamaNumber = mamaNumber, actions = actions)
}

@OptIn(ExperimentalMaterial3Api::class)
@Composable
private fun SetupContent(state: SetupUiState, mamaNumber: String?, actions: SetupActions) {
    Scaffold(
        topBar = { CenterAlignedTopAppBar(title = { MamaWordmark(compact = true) }) },
        bottomBar = { BottomActions(state, actions) },
    ) { padding ->
        Column(
            modifier = Modifier
                .fillMaxSize()
                .padding(padding)
                .verticalScroll(rememberScrollState())
                .padding(horizontal = MamaSpacing.screen, vertical = MamaSpacing.l),
            verticalArrangement = Arrangement.spacedBy(MamaSpacing.m),
        ) {
            when (state.phase) {
                SetupPhase.PERMISSIONS -> PermissionsPhase(state.permissions)
                SetupPhase.FORM -> FormPhase(state, mamaNumber, actions)
                SetupPhase.FINISHING -> FinishingPhase(state)
                SetupPhase.DONE -> DonePhase(state, mamaNumber)
            }
        }
    }
}

@Composable
private fun BottomActions(state: SetupUiState, actions: SetupActions) {
    Surface(color = MaterialTheme.colorScheme.surface) {
        Column(
            modifier = Modifier
                .fillMaxWidth()
                // Keyboard + navigation bar: the form's text fields sit above this bar.
                .windowInsetsPadding(WindowInsets.safeDrawing.only(WindowInsetsSides.Bottom))
                .padding(horizontal = MamaSpacing.screen, vertical = MamaSpacing.m),
            horizontalAlignment = Alignment.CenterHorizontally,
            verticalArrangement = Arrangement.spacedBy(MamaSpacing.xs),
        ) {
            val buttonModifier = Modifier.fillMaxWidth()
            val buttonPadding = PaddingValues(vertical = MamaSpacing.m)
            when (state.phase) {
                SetupPhase.PERMISSIONS -> {
                    val step = state.permissions.current
                    if (step == null) {
                        Button(onClick = actions.onContinueToForm, modifier = buttonModifier, contentPadding = buttonPadding) {
                            Text("Continuar")
                        }
                    } else {
                        val status = state.permissions.statusOf(step)
                        Button(onClick = { actions.onStepAction(step) }, modifier = buttonModifier, contentPadding = buttonPadding) {
                            Text(
                                when (status) {
                                    StepStatus.DENIED -> "Intentar de nuevo"
                                    StepStatus.BLOCKED -> "Abrir ajustes de la app"
                                    else -> copyFor(step).action
                                },
                            )
                        }
                        if (step.isSystemScreen || status == StepStatus.DENIED || status == StepStatus.BLOCKED) {
                            TextButton(onClick = { actions.onSkip(step) }) {
                                Text(if (step.isSystemScreen) "Saltar este paso" else "Seguir sin este permiso")
                            }
                        }
                    }
                }
                SetupPhase.FORM -> Button(onClick = actions.onSubmit, modifier = buttonModifier, contentPadding = buttonPadding) {
                    Text("Guardar y activar")
                }
                SetupPhase.FINISHING -> {
                    val busy = state.contact.status == TaskStatus.RUNNING || state.registration.status == TaskStatus.RUNNING
                    Button(
                        onClick = actions.onRetry,
                        enabled = !busy && state.anyTaskFailed,
                        modifier = buttonModifier,
                        contentPadding = buttonPadding,
                    ) {
                        Text(if (busy || !state.anyTaskFailed) "Activando…" else "Reintentar")
                    }
                    if (!busy && state.contact.needsAppSettings) {
                        TextButton(onClick = actions.onOpenAppSettings) { Text("Abrir ajustes de la app") }
                    }
                    if (!busy && state.anyTaskFailed) {
                        TextButton(onClick = actions.onEdit) { Text("Cambiar mis datos") }
                    }
                }
                SetupPhase.DONE -> {
                    OutlinedButton(onClick = actions.onEdit, modifier = buttonModifier, contentPadding = buttonPadding) {
                        Text("Cambiar mis datos")
                    }
                    // Only runtime permissions can be read back; the MIUI screens cannot.
                    val missing = state.permissions.steps.any {
                        !it.isSystemScreen && state.permissions.statusOf(it) != StepStatus.GRANTED
                    }
                    if (missing) {
                        TextButton(onClick = actions.onReviewPermissions) { Text("Revisar permisos") }
                    }
                }
            }
        }
    }
}

@Composable
private fun Header(title: String, body: String) {
    Column(verticalArrangement = Arrangement.spacedBy(MamaSpacing.s)) {
        Text(text = title, style = MaterialTheme.typography.headlineMedium, color = MaterialTheme.colorScheme.onBackground)
        Text(text = body, style = MaterialTheme.typography.bodyLarge, color = MaterialTheme.colorScheme.onSurfaceVariant)
    }
}

@Composable
private fun PermissionsPhase(flow: PermissionFlow) {
    val current = flow.current
    val currentStatus = current?.let(flow::statusOf)
    Header(
        title = "Tu familia, a un gesto",
        body = when (currentStatus) {
            StepStatus.DENIED -> "Sin este permiso MAMÁ funciona a medias. Puedes intentarlo de nuevo o seguir sin él."
            StepStatus.BLOCKED -> "Android ya no muestra el aviso. Actívalo en Ajustes > Permisos y vuelve aquí."
            null -> "Listo. Ahora cuéntanos a quién avisar."
            else -> "Si algún día estás en peligro, presionas volumen arriba-abajo-arriba y Mamá te llama. " +
                "Quien esté a tu lado solo ve una llamada normal."
        },
    )
    SectionLabel("Permisos, en este orden")
    GroupCard {
        flow.steps.forEachIndexed { index, step ->
            if (index > 0) HorizontalDivider(color = MaterialTheme.colorScheme.outlineVariant)
            val copy = copyFor(step)
            val status = flow.statusOf(step)
            val isCurrent = step == current
            val supporting = when {
                status == StepStatus.DENIED || status == StepStatus.BLOCKED -> copy.denied
                status == StepStatus.SKIPPED -> "Lo saltaste. ${copy.why}"
                // Background location is only granted on top of the foreground one.
                isCurrent && step == SetupStep.BACKGROUND_LOCATION &&
                    flow.statusOf(SetupStep.LOCATION) != StepStatus.GRANTED ->
                    "Primero necesitas permitir la ubicación mientras usas la app."
                isCurrent && copy.hint != null -> "${copy.why} ${copy.hint}"
                else -> copy.why
            }
            StatusRow(
                number = index + 1,
                title = copy.title,
                supporting = supporting,
                state = when (status) {
                    StepStatus.GRANTED, StepStatus.VISITED -> RowState.DONE
                    StepStatus.DENIED, StepStatus.BLOCKED -> RowState.FAILED
                    StepStatus.SKIPPED -> RowState.SKIPPED
                    StepStatus.PENDING -> RowState.PENDING
                },
            )
        }
    }
}

@Composable
private fun FormPhase(state: SetupUiState, mamaNumber: String?, actions: SetupActions) {
    val form = state.form
    val errors = state.formErrors.takeIf { state.showFormErrors }
    Header(
        title = "¿A quién avisamos?",
        body = "Mamá te llama a tu número. Si dices tu frase en la llamada, tu familia recibe dónde estás.",
    )

    SectionLabel("Tu número")
    PhoneField(
        value = form.ownNumber,
        onValueChange = { actions.onFormChange(form.copy(ownNumber = it)) },
        label = "Tu número",
        error = errors?.ownNumber,
    )

    SectionLabel("Tu familia")
    GroupCard {
        Column(
            modifier = Modifier.padding(MamaSpacing.m),
            verticalArrangement = Arrangement.spacedBy(MamaSpacing.s),
        ) {
            form.familyNumbers.forEachIndexed { index, number ->
                Row(verticalAlignment = Alignment.Top) {
                    PhoneField(
                        value = number,
                        onValueChange = { typed ->
                            actions.onFormChange(form.copy(familyNumbers = form.familyNumbers.toMutableList().also { it[index] = typed }))
                        },
                        label = "Número ${index + 1}",
                        error = errors?.familyNumbers?.getOrNull(index),
                        modifier = Modifier.weight(1f),
                    )
                    if (form.familyNumbers.size > 1) {
                        TextButton(
                            onClick = {
                                actions.onFormChange(form.copy(familyNumbers = form.familyNumbers.filterIndexed { i, _ -> i != index }))
                            },
                            modifier = Modifier.padding(top = MamaSpacing.s),
                        ) { Text("Quitar") }
                    }
                }
            }
            if (form.familyNumbers.size < SetupForm.MAX_FAMILY_NUMBERS) {
                TextButton(onClick = { actions.onFormChange(form.copy(familyNumbers = form.familyNumbers + "")) }) {
                    Text("Agregar otro número")
                }
            }
        }
    }

    SectionLabel("Tu frase secreta")
    OutlinedTextField(
        value = form.duressPhrase,
        onValueChange = { actions.onFormChange(form.copy(duressPhrase = it)) },
        modifier = Modifier.fillMaxWidth(),
        label = { Text("Tu frase") },
        isError = errors?.duressPhrase != null,
        supportingText = {
            Text(errors?.duressPhrase ?: "Si la dices en la llamada, tu familia recibe dónde estás. Para quien te escucha, es una frase cualquiera.")
        },
        keyboardOptions = KeyboardOptions(capitalization = KeyboardCapitalization.Sentences, imeAction = ImeAction.Done),
    )

    SectionLabel("El contacto Mamá")
    MamaCard(
        supporting = if (mamaNumber != null) {
            "Guardamos $mamaNumber en tus contactos para que su llamada se vea normal."
        } else {
            "La guardamos en tus contactos para que su llamada se vea normal."
        },
    )
}

@Composable
private fun FinishingPhase(state: SetupUiState) {
    Header(
        title = if (state.anyTaskFailed) "Falta un paso" else "Activando MAMÁ",
        body = if (state.anyTaskFailed) "Revisa lo que falló y vuelve a intentarlo." else "Esto toma unos segundos.",
    )
    SectionLabel("Último paso")
    GroupCard {
        TaskRow(1, "Guardar a Mamá en tus contactos", "Para que su llamada se vea normal.", state.contact)
        HorizontalDivider(color = MaterialTheme.colorScheme.outlineVariant)
        TaskRow(2, "Registrar este teléfono", "Para que Mamá sepa a quién llamar y a quién avisar.", state.registration)
    }
}

@Composable
private fun TaskRow(number: Int, title: String, why: String, task: SetupTask) {
    StatusRow(
        number = number,
        title = title,
        supporting = if (task.status == TaskStatus.FAILED) task.error ?: why else why,
        state = when (task.status) {
            TaskStatus.PENDING -> RowState.PENDING
            TaskStatus.RUNNING -> RowState.RUNNING
            TaskStatus.DONE -> RowState.DONE
            TaskStatus.FAILED -> RowState.FAILED
        },
    )
}

@Composable
private fun DonePhase(state: SetupUiState, mamaNumber: String?) {
    Header(
        title = "MAMÁ está lista",
        body = "Si algún día estás en peligro, presiona volumen arriba-abajo-arriba. Mamá te llama en segundos.",
    )
    SectionLabel("Tu familia")
    GroupCard {
        state.form.familyNumbers.forEachIndexed { index, number ->
            if (index > 0) HorizontalDivider(color = MaterialTheme.colorScheme.outlineVariant)
            ListItem(
                headlineContent = { Text(PhoneNumbers.normalize(number)?.let(PhoneNumbers::display) ?: number) },
                supportingContent = { Text("Recibe tu ubicación si la pides.") },
                colors = ListItemDefaults.colors(containerColor = MaterialTheme.colorScheme.surfaceContainerLow),
            )
        }
    }
    SectionLabel("Tu frase secreta")
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
            Text(text = "«${state.form.duressPhrase.trim().replaceFirstChar(Char::uppercase)}»", style = MaterialTheme.typography.titleLarge)
            Text(
                text = "Si la dices en la llamada, tu familia recibe dónde estás. Para quien te escucha, es una frase cualquiera.",
                style = MaterialTheme.typography.bodyMedium,
            )
        }
    }
    SectionLabel("El contacto Mamá")
    MamaCard(supporting = listOfNotNull(mamaNumber, "Ya está en tus contactos.").joinToString(" · "))
}

@Composable
private fun PhoneField(
    value: String,
    onValueChange: (String) -> Unit,
    label: String,
    error: String?,
    modifier: Modifier = Modifier.fillMaxWidth(),
) {
    OutlinedTextField(
        value = value,
        onValueChange = onValueChange,
        modifier = modifier,
        label = { Text(label) },
        placeholder = { Text("0414 123 4567") },
        isError = error != null,
        supportingText = error?.let { { Text(it) } },
        singleLine = true,
        keyboardOptions = KeyboardOptions(keyboardType = KeyboardType.Phone, imeAction = ImeAction.Next),
    )
}

@Composable
private fun GroupCard(content: @Composable () -> Unit) {
    Card(colors = CardDefaults.cardColors(containerColor = MaterialTheme.colorScheme.surfaceContainerLow)) {
        content()
    }
}

@Composable
private fun MamaCard(supporting: String) {
    Card(
        colors = CardDefaults.cardColors(
            containerColor = MaterialTheme.colorScheme.primaryContainer,
            contentColor = MaterialTheme.colorScheme.onPrimaryContainer,
        ),
    ) {
        ListItem(
            headlineContent = { Text(MAMA_CONTACT_NAME) },
            supportingContent = { Text(supporting) },
            leadingContent = { Initial("M") },
            colors = ListItemDefaults.colors(
                containerColor = MaterialTheme.colorScheme.primaryContainer,
                headlineColor = MaterialTheme.colorScheme.onPrimaryContainer,
                supportingColor = MaterialTheme.colorScheme.onPrimaryContainer,
            ),
        )
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

private enum class RowState(val spoken: String) {
    DONE("listo"),
    RUNNING("en curso"),
    PENDING("pendiente"),
    FAILED("falta"),
    SKIPPED("saltado"),
}

/** A numbered step with the UI_RULES step states: done, running, pending, failed (and skipped, shown as pending). */
@Composable
private fun StatusRow(number: Int, title: String, supporting: String, state: RowState) {
    val failed = state == RowState.FAILED
    val colors = MaterialTheme.colorScheme
    ListItem(
        // One TalkBack node per step: "Ubicación, Primero mientras..., listo".
        modifier = Modifier.semantics(mergeDescendants = true) { stateDescription = state.spoken },
        headlineContent = { Text(title) },
        supportingContent = { Text(supporting) },
        leadingContent = {
            Box(modifier = Modifier.size(32.dp), contentAlignment = Alignment.Center) {
                when (state) {
                    RowState.RUNNING -> CircularProgressIndicator(modifier = Modifier.size(28.dp), strokeWidth = 3.dp)
                    RowState.DONE -> Badge(container = colors.primary, content = colors.onPrimary) {
                        Icon(painterResource(R.drawable.ic_check), contentDescription = null, modifier = Modifier.size(20.dp))
                    }
                    RowState.FAILED -> Badge(container = colors.error, content = colors.onError) {
                        Text("!", style = MaterialTheme.typography.labelLarge)
                    }
                    RowState.PENDING, RowState.SKIPPED -> Badge(
                        container = colors.surfaceContainerHighest,
                        content = colors.onSurfaceVariant,
                        border = BorderStroke(1.dp, colors.outline),
                    ) {
                        Text(number.toString(), style = MaterialTheme.typography.labelLarge)
                    }
                }
            }
        },
        colors = ListItemDefaults.colors(
            containerColor = if (failed) colors.errorContainer else colors.surfaceContainerLow,
            headlineColor = if (failed) colors.onErrorContainer else colors.onSurface,
            supportingColor = if (failed) colors.onErrorContainer else colors.onSurfaceVariant,
        ),
    )
}

@Composable
private fun Badge(container: Color, content: Color, border: BorderStroke? = null, inner: @Composable () -> Unit) {
    Surface(
        modifier = Modifier.size(32.dp),
        shape = CircleShape,
        color = container,
        contentColor = content,
        border = border,
    ) {
        Box(contentAlignment = Alignment.Center) { inner() }
    }
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

private val previewSteps = SetupStep.forDevice(sdkInt = 36, isXiaomi = true)

private fun previewState(phase: SetupPhase): SetupUiState {
    var flow = PermissionFlow.start(previewSteps)
        .afterRequest(SetupStep.NOTIFICATIONS, granted = true, canAskAgain = true)
        .afterRequest(SetupStep.LOCATION, granted = true, canAskAgain = true)
        .afterRequest(SetupStep.BACKGROUND_LOCATION, granted = false, canAskAgain = true)
    if (phase != SetupPhase.PERMISSIONS) flow = PermissionFlow(previewSteps, previewSteps.associateWith { StepStatus.GRANTED })
    return SetupUiState(
        phase = phase,
        permissions = flow,
        form = SetupForm(ownNumber = "0414 123 4567", familyNumbers = listOf("+58 424 111 2233", "0412 555 6677")),
        contact = SetupTask(TaskStatus.DONE),
        registration = when (phase) {
            SetupPhase.FINISHING -> SetupTask(TaskStatus.FAILED, "No pudimos conectar con el servidor. Revisa tu conexión e inténtalo de nuevo.")
            SetupPhase.DONE -> SetupTask(TaskStatus.DONE)
            else -> SetupTask()
        },
    )
}

@Preview(showBackground = true, heightDp = 1400)
@Composable
private fun SetupPermissionsPreview() {
    MamaTheme { SetupContent(previewState(SetupPhase.PERMISSIONS), "+1 555 010 0123", SetupActions()) }
}

@Preview(showBackground = true, heightDp = 1400, uiMode = Configuration.UI_MODE_NIGHT_YES)
@Composable
private fun SetupFormDarkPreview() {
    MamaTheme { SetupContent(previewState(SetupPhase.FORM), "+1 555 010 0123", SetupActions()) }
}

@Preview(showBackground = true)
@Composable
private fun SetupFinishingPreview() {
    MamaTheme { SetupContent(previewState(SetupPhase.FINISHING), "+1 555 010 0123", SetupActions()) }
}

@Preview(showBackground = true, heightDp = 1000)
@Composable
private fun SetupDonePreview() {
    MamaTheme { SetupContent(previewState(SetupPhase.DONE), "+1 555 010 0123", SetupActions()) }
}
