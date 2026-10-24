package com.team27.mama.setup

import android.Manifest
import android.os.Build
import com.team27.mama.BuildConfig

/**
 * One step of the permission part of first-run setup. A step with
 * [permissions] is a runtime permission dialog; a step without them is a
 * system settings screen the person turns on by hand (see SystemScreens.kt).
 */
enum class SetupStep(
    val permissions: List<String> = emptyList(),
    /** The step only exists from this API level (the permission is implicit before it). */
    val minSdk: Int = Build.VERSION_CODES.BASE,
    /** The step is a MIUI/HyperOS screen with no equivalent on other phones. */
    val xiaomiOnly: Boolean = false,
) {
    NOTIFICATIONS(listOf(Manifest.permission.POST_NOTIFICATIONS), minSdk = Build.VERSION_CODES.TIRAMISU),
    LOCATION(listOf(Manifest.permission.ACCESS_FINE_LOCATION, Manifest.permission.ACCESS_COARSE_LOCATION)),

    // Android 11+ only grants background location in its own request, after the foreground one.
    BACKGROUND_LOCATION(listOf(Manifest.permission.ACCESS_BACKGROUND_LOCATION), minSdk = Build.VERSION_CODES.Q),
    MICROPHONE(listOf(Manifest.permission.RECORD_AUDIO)),
    CONTACTS(listOf(Manifest.permission.READ_CONTACTS, Manifest.permission.WRITE_CONTACTS)),
    // The offline emergency (#58) also reads the server's ack SMS: same group, one dialog.
    SMS(
        listOf(Manifest.permission.SEND_SMS) +
            if (BuildConfig.OFFLINE_EMERGENCY_ENABLED) listOf(Manifest.permission.RECEIVE_SMS) else emptyList(),
    ),
    AUTOSTART(xiaomiOnly = true),
    BATTERY,
    POPUPS(xiaomiOnly = true),
    ACCESSIBILITY,
    ;

    val isSystemScreen: Boolean get() = permissions.isEmpty()

    /**
     * Whether [granted] covers this step: every permission, except location,
     * which counts with only "approximate" (the person's choice on Android 12+).
     */
    fun isGrantedBy(granted: (String) -> Boolean): Boolean =
        if (this == LOCATION) permissions.any(granted) else permissions.isNotEmpty() && permissions.all(granted)

    companion object {
        /**
         * The steps this phone shows, in the product order (docs/MAMA-summary.md):
         * runtime permissions first, then the Xiaomi and battery screens, and
         * accessibility always last.
         */
        fun forDevice(sdkInt: Int, isXiaomi: Boolean): List<SetupStep> =
            entries.filter { sdkInt >= it.minSdk && (isXiaomi || !it.xiaomiOnly) }
    }
}

/** Where one step stands. */
enum class StepStatus(val blocksProgress: Boolean) {
    PENDING(true),
    GRANTED(false),

    /** Denied, and the system will still show the dialog again. */
    DENIED(true),

    /** Denied with "don't ask again": only the app settings screen can grant it now. */
    BLOCKED(true),

    /** The person chose to continue without it after a denial. */
    SKIPPED(false),

    /** A settings screen we cannot read back (MIUI autostart, pop-ups) was opened. */
    VISITED(false),
}

/**
 * The permission part of setup as plain state, so the order and the
 * transitions are unit-tested. The current step is the first one still
 * blocking; the flow is finished when none is.
 */
data class PermissionFlow(val steps: List<SetupStep>, val statuses: Map<SetupStep, StepStatus>) {

    val current: SetupStep? get() = steps.firstOrNull { statusOf(it).blocksProgress }

    val isFinished: Boolean get() = current == null

    fun statusOf(step: SetupStep): StepStatus = statuses[step] ?: StepStatus.PENDING

    /** The answer to a runtime permission request for [step]. */
    fun afterRequest(step: SetupStep, granted: Boolean, canAskAgain: Boolean): PermissionFlow = withStatus(
        step,
        when {
            granted -> StepStatus.GRANTED
            canAskAgain -> StepStatus.DENIED
            else -> StepStatus.BLOCKED
        },
    )

    /** The person came back from the settings screen of a system step. */
    fun afterVisit(step: SetupStep): PermissionFlow =
        if (statusOf(step) == StepStatus.GRANTED) this else withStatus(step, StepStatus.VISITED)

    fun skip(step: SetupStep): PermissionFlow = withStatus(step, StepStatus.SKIPPED)

    /**
     * Re-reads what the system says, for example after the person granted a
     * permission from app settings. [isGranted] answers null when a step
     * cannot be read back; that step keeps its status. A step that was
     * granted and no longer is (revoked) goes back to pending.
     */
    fun refreshed(isGranted: (SetupStep) -> Boolean?): PermissionFlow = copy(
        statuses = steps.associateWith { step ->
            val status = statusOf(step)
            when (isGranted(step)) {
                true -> StepStatus.GRANTED
                false -> if (status == StepStatus.GRANTED) StepStatus.PENDING else status
                null -> status
            }
        },
    )

    private fun withStatus(step: SetupStep, status: StepStatus) = copy(statuses = statuses + (step to status))

    companion object {
        fun start(steps: List<SetupStep>): PermissionFlow =
            PermissionFlow(steps, steps.associateWith { StepStatus.PENDING })
    }
}
