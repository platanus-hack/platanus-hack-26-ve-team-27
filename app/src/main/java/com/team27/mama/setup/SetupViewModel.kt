package com.team27.mama.setup

import android.Manifest
import android.app.Application
import android.content.pm.PackageManager
import android.os.PowerManager
import android.provider.Settings
import androidx.core.content.ContextCompat
import androidx.lifecycle.AndroidViewModel
import androidx.lifecycle.viewModelScope
import com.team27.mama.BuildConfig
import com.team27.mama.MamaApp
import com.team27.mama.onboarding.OnboardingResult
import com.team27.mama.onboarding.userMessage
import kotlinx.coroutines.CancellationException
import kotlinx.coroutines.Dispatchers
import kotlinx.coroutines.flow.MutableStateFlow
import kotlinx.coroutines.flow.StateFlow
import kotlinx.coroutines.flow.asStateFlow
import kotlinx.coroutines.flow.update
import kotlinx.coroutines.launch
import kotlinx.coroutines.withContext

/** The four parts of first-run setup, in order. */
enum class SetupPhase { PERMISSIONS, FORM, FINISHING, DONE }

enum class TaskStatus { PENDING, RUNNING, DONE, FAILED }

/** One of the two things setup does at the end. [error] is Spanish, shown when [status] is FAILED. */
data class SetupTask(
    val status: TaskStatus = TaskStatus.PENDING,
    val error: String? = null,
    /** The fix is in the app settings screen (a permission was denied for good). */
    val needsAppSettings: Boolean = false,
)

data class SetupUiState(
    val phase: SetupPhase,
    val permissions: PermissionFlow,
    val form: SetupForm,
    val showFormErrors: Boolean = false,
    val contact: SetupTask = SetupTask(),
    val registration: SetupTask = SetupTask(),
    /** False when the person skipped contacts: Mamá then calls as a bare number. */
    val mamaInContacts: Boolean = true,
) {
    val formErrors: SetupFormErrors get() = form.errors()
    val anyTaskFailed: Boolean get() = contact.status == TaskStatus.FAILED || registration.status == TaskStatus.FAILED
}

/**
 * First-run setup (#15): permissions in order, the form, then saving "Mamá"
 * and registering the device. A finished setup opens on [SetupPhase.DONE].
 */
class SetupViewModel(application: Application) : AndroidViewModel(application) {
    private val app = application
    private val store = SetupStore(SharedPreferencesStore.forSetup(application))
    private val contacts = MamaContactWriter(application.contentResolver)
    private val client = RegistrationClient(BuildConfig.SERVER_URL)
    private val graph get() = (app as MamaApp).graph
    private val isXiaomi = SystemScreens.isXiaomi()

    private val _state = MutableStateFlow(initialState())
    val state: StateFlow<SetupUiState> = _state.asStateFlow()

    /** True while the contact and registration tasks run, so a double tap does not start them twice. */
    private var running = false

    private fun initialState(): SetupUiState {
        val steps = SetupStep.forDevice(android.os.Build.VERSION.SDK_INT, isXiaomi)
        val saved = SetupForm(
            ownNumber = store.ownNumber.orEmpty(),
            familyNumbers = store.familyNumbers.ifEmpty { listOf("") },
            duressPhrase = store.duressPhrase ?: SetupForm.DEFAULT_DURESS_PHRASE,
        )
        val done = store.setupCompleted
        return SetupUiState(
            phase = if (done) SetupPhase.DONE else SetupPhase.PERMISSIONS,
            permissions = PermissionFlow.start(steps).restored(store.settledSteps).refreshed(::isGranted),
            form = saved,
            contact = if (done) SetupTask(TaskStatus.DONE) else SetupTask(),
            registration = if (done) SetupTask(TaskStatus.DONE) else SetupTask(),
            mamaInContacts = !done || store.mamaRawContactId != null,
        )
    }

    /** Call on every resume: the person may have granted something in Settings meanwhile. */
    fun onResume() {
        updatePermissions { it.refreshed(::isGranted) }
    }

    fun onPermissionResult(step: SetupStep, granted: Boolean, canAskAgain: Boolean) {
        updatePermissions { it.afterRequest(step, granted, canAskAgain) }
    }

    /**
     * Marked visited as it opens, not when the app resumes: MIUI often kills the
     * process while its Security Center is in front, and the visit must survive that.
     */
    fun onSystemScreenOpened(step: SetupStep) {
        updatePermissions { it.afterVisit(step) }
    }

    fun skip(step: SetupStep) {
        updatePermissions { it.skip(step) }
    }

    /** Applies [change] and saves the statuses the system cannot give back after a restart. */
    private fun updatePermissions(change: (PermissionFlow) -> PermissionFlow) {
        _state.update { it.copy(permissions = change(it.permissions)) }
        val settled = _state.value.permissions.settled
        if (settled != store.settledSteps) store.settledSteps = settled
    }

    fun continueToForm() {
        _state.update { it.copy(phase = SetupPhase.FORM) }
    }

    fun onFormChange(form: SetupForm) {
        _state.update { it.copy(form = form) }
    }

    fun submitForm() {
        if (running) return
        val valid = _state.value.form.validated()
        if (valid == null) {
            _state.update { it.copy(showFormErrors = true) }
            return
        }
        store.ownNumber = valid.ownNumber
        store.familyNumbers = valid.familyNumbers
        store.duressPhrase = valid.duressPhrase
        // Until the new data reaches the server, a relaunch must not open on "done".
        store.setupCompleted = false
        _state.update {
            it.copy(phase = SetupPhase.FINISHING, contact = SetupTask(), registration = SetupTask())
        }
        runPendingTasks()
    }

    fun retry() = runPendingTasks()

    /** From the done screen: go back to the permissions that are still missing. */
    fun reviewPermissions() {
        _state.update { it.copy(phase = SetupPhase.PERMISSIONS) }
    }

    /** From the done screen: change the numbers or the phrase. Saving runs both tasks again. */
    fun edit() {
        _state.update { it.copy(phase = SetupPhase.FORM, showFormErrors = false) }
    }

    private fun runPendingTasks() {
        if (running) return
        running = true
        viewModelScope.launch {
            try {
                if (_state.value.contact.status != TaskStatus.DONE) {
                    _state.update { it.copy(contact = SetupTask(TaskStatus.RUNNING)) }
                    val contact = saveMamaContact()
                    _state.update { it.copy(contact = contact) }
                }
                if (_state.value.registration.status != TaskStatus.DONE) {
                    _state.update { it.copy(registration = SetupTask(TaskStatus.RUNNING)) }
                    val registration = register()
                    _state.update { it.copy(registration = registration) }
                }
                val state = _state.value
                if (state.contact.status == TaskStatus.DONE && state.registration.status == TaskStatus.DONE) {
                    store.setupCompleted = true
                    _state.update { it.copy(phase = SetupPhase.DONE) }
                }
            } finally {
                running = false
            }
        }
    }

    private suspend fun saveMamaContact(): SetupTask {
        val number = PhoneNumbers.normalize(BuildConfig.TWILIO_NUMBER)
            ?: return failed("Esta versión de la app no trae el número de Mamá. Pide una versión nueva al equipo.")
        if (!hasPermission(Manifest.permission.WRITE_CONTACTS) || !hasPermission(Manifest.permission.READ_CONTACTS)) {
            // "Seguir sin este permiso" promised setup goes on: the call still works, as an unknown number.
            if (_state.value.permissions.statusOf(SetupStep.CONTACTS) == StepStatus.SKIPPED) {
                _state.update { it.copy(mamaInContacts = false) }
                return SetupTask(TaskStatus.DONE)
            }
            return failed("Necesitamos el permiso de contactos para guardar a Mamá.", needsAppSettings = true)
        }
        return try {
            val id = withContext(Dispatchers.IO) { contacts.upsert(number, store.mamaRawContactId) }
            store.mamaRawContactId = id
            _state.update { it.copy(mamaInContacts = true) }
            SetupTask(TaskStatus.DONE)
        } catch (e: CancellationException) {
            throw e
        } catch (_: Exception) {
            // SecurityException, OperationApplicationException, RemoteException: the provider said no.
            failed("No pudimos guardar a Mamá en tus contactos. Inténtalo de nuevo.")
        }
    }

    private suspend fun register(): SetupTask {
        val registration = store.registration(BuildConfig.VERSION_NAME)
            ?: return failed("Faltan tus datos. Vuelve al paso anterior.")
        return when (val result = client.register(registration)) {
            RegistrationResult.Registered -> if (graph.offlineEmergency) finishOnboarding() else SetupTask(TaskStatus.DONE)
            is RegistrationResult.Failed -> failed(result.message)
        }
    }

    /**
     * Last step of setup, only with the offline emergency flag (#55): the phone must hold the server keys (GET /config)
     * before it counts as set up. The gate passes at once when they are cached,
     * which keeps the offline demo working.
     */
    private suspend fun finishOnboarding(): SetupTask {
        val gate = graph.onboardingGate
        val result = withContext(Dispatchers.IO) { gate.finish() }
        return if (result == OnboardingResult.Done) SetupTask(TaskStatus.DONE) else failed(result.userMessage().orEmpty())
    }

    private fun failed(message: String, needsAppSettings: Boolean = false) =
        SetupTask(TaskStatus.FAILED, message, needsAppSettings)

    private fun hasPermission(permission: String) =
        ContextCompat.checkSelfPermission(app, permission) == PackageManager.PERMISSION_GRANTED

    /** What the system says about [step]; null when it cannot be read back (MIUI screens). */
    private fun isGranted(step: SetupStep): Boolean? = when (step) {
        SetupStep.BATTERY ->
            app.getSystemService(PowerManager::class.java)?.isIgnoringBatteryOptimizations(app.packageName)
        SetupStep.ACCESSIBILITY ->
            Settings.Secure.getString(app.contentResolver, Settings.Secure.ENABLED_ACCESSIBILITY_SERVICES)
                ?.split(':')
                ?.any { it.startsWith(app.packageName + "/") }
                ?: false
        SetupStep.AUTOSTART, SetupStep.POPUPS -> null
        else -> step.isGrantedBy(::hasPermission)
    }
}
