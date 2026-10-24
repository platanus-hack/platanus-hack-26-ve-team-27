package com.team27.mama.setup

import android.content.ActivityNotFoundException
import android.content.ComponentName
import android.content.Context
import android.content.Intent
import android.net.Uri
import android.os.Build
import android.provider.Settings

/**
 * A settings screen to try opening, as plain data so the fallback order is
 * unit-tested. [className] needs [packageName]; [withPackageUri] adds
 * `package:<our package>` as the intent data.
 */
data class ScreenTarget(
    val action: String? = null,
    val packageName: String? = null,
    val className: String? = null,
    val withPackageUri: Boolean = false,
    val extras: Map<String, String> = emptyMap(),
)

/**
 * The screens each system step opens, best first. MIUI/HyperOS moves its
 * screens between versions, so every list ends with the generic app settings
 * screen (docs/os-note.md, "Xiaomi background setup").
 */
object SystemScreens {
    private const val MIUI_SECURITY = "com.miui.securitycenter"
    private const val MIUI_POWER = "com.miui.powerkeeper"
    private const val MIUI_PERMISSION_EDITOR = "miui.intent.action.APP_PERM_EDITOR"

    val appSettings = ScreenTarget(action = Settings.ACTION_APPLICATION_DETAILS_SETTINGS, withPackageUri = true)

    fun targetsFor(step: SetupStep, appPackage: String, appLabel: String, isXiaomi: Boolean): List<ScreenTarget> {
        val specific = when (step) {
            SetupStep.AUTOSTART -> listOf(
                ScreenTarget(
                    packageName = MIUI_SECURITY,
                    className = "com.miui.permcenter.autostart.AutoStartManagementActivity",
                ),
            )
            SetupStep.BATTERY -> listOfNotNull(
                // MIUI's own "Battery saver" page for the app, where "No restrictions" lives.
                ScreenTarget(
                    packageName = MIUI_POWER,
                    className = "com.miui.powerkeeper.ui.HiddenAppsConfigActivity",
                    extras = mapOf("package_name" to appPackage, "package_label" to appLabel),
                ).takeIf { isXiaomi },
                ScreenTarget(action = Settings.ACTION_REQUEST_IGNORE_BATTERY_OPTIMIZATIONS, withPackageUri = true),
                ScreenTarget(action = Settings.ACTION_IGNORE_BATTERY_OPTIMIZATION_SETTINGS),
            )
            // "Display pop-up windows while running in the background" and "Open new windows".
            SetupStep.POPUPS -> listOf(
                ScreenTarget(
                    action = MIUI_PERMISSION_EDITOR,
                    packageName = MIUI_SECURITY,
                    className = "com.miui.permcenter.permissions.PermissionsEditorActivity",
                    extras = mapOf("extra_pkgname" to appPackage),
                ),
                ScreenTarget(
                    action = MIUI_PERMISSION_EDITOR,
                    packageName = MIUI_SECURITY,
                    className = "com.miui.permcenter.permissions.AppPermissionsEditorActivity",
                    extras = mapOf("extra_pkgname" to appPackage),
                ),
            )
            SetupStep.ACCESSIBILITY -> listOf(ScreenTarget(action = Settings.ACTION_ACCESSIBILITY_SETTINGS))
            else -> emptyList()
        }
        return specific + appSettings
    }

    /** True for Xiaomi, Redmi and POCO phones (all MIUI or HyperOS). */
    fun isXiaomi(manufacturer: String = Build.MANUFACTURER, brand: String = Build.BRAND): Boolean =
        listOf(manufacturer, brand).any { it.lowercase() in setOf("xiaomi", "redmi", "poco") }
}

/**
 * Opens the first of [targets] that the phone has. Missing or locked screens
 * (ActivityNotFoundException, SecurityException on a non-exported MIUI
 * activity) are skipped instead of crashing. Returns false when none opened.
 */
fun Context.openFirst(targets: List<ScreenTarget>): Boolean = targets.any { target ->
    val intent = Intent().apply {
        target.action?.let { action = it }
        if (target.packageName != null && target.className != null) {
            component = ComponentName(target.packageName, target.className)
        }
        if (target.withPackageUri) data = Uri.fromParts("package", packageName, null)
        target.extras.forEach { (key, value) -> putExtra(key, value) }
        if (this@openFirst !is android.app.Activity) addFlags(Intent.FLAG_ACTIVITY_NEW_TASK)
    }
    try {
        startActivity(intent)
        true
    } catch (_: ActivityNotFoundException) {
        false
    } catch (_: SecurityException) {
        false
    }
}
