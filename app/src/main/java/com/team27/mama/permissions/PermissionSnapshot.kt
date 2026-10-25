package com.team27.mama.permissions

import android.Manifest
import android.os.Build
import com.team27.mama.contract.DeviceState
import com.team27.mama.contract.PermissionKeys
import java.time.Instant

/**
 * What [AndroidPermissions] read from the system, as plain values, so the
 * snapshot's rules are unit-tested without Android. [granted] holds the
 * runtime permissions (`Manifest.permission` names) the app has.
 */
data class PermissionReadings(
    val sdkInt: Int,
    val granted: Set<String>,
    /** NotificationManagerCompat.areNotificationsEnabled(). */
    val notificationsEnabled: Boolean,
    /** The volume gesture's accessibility service is on. */
    val accessibilityServiceOn: Boolean,
    /** PowerManager.isIgnoringBatteryOptimizations(); false when the system has no PowerManager. */
    val ignoringBatteryOptimizations: Boolean,
    /** NotificationManager.canUseFullScreenIntent(); null before Android 14, where it does not exist. */
    val canUseFullScreenIntent: Boolean?,
    /** PackageManager.isAutoRevokeWhitelisted; null before Android 11, where it does not exist. */
    val autoRevokeExempt: Boolean?,
)

/**
 * Capa 1 of #125: the phone's permissions as the contract's
 * [DeviceState.permissions], read at the gesture without asking for anything.
 */
object PermissionSnapshot {
    /**
     * One entry per [PermissionKeys] name that applies to this phone, in
     * [PermissionKeys.ALL] order. Before Android 10 background location comes
     * with the foreground one; full_screen_intent (Android 14+) and
     * hibernation_exempt (Android 11+) are left out on older phones.
     */
    fun permissions(r: PermissionReadings): Map<String, Boolean> {
        val fine = Manifest.permission.ACCESS_FINE_LOCATION in r.granted
        val foreground = fine || Manifest.permission.ACCESS_COARSE_LOCATION in r.granted
        val values = mapOf(
            PermissionKeys.NOTIFICATIONS to r.notificationsEnabled,
            PermissionKeys.LOCATION_FINE to fine,
            PermissionKeys.LOCATION_BACKGROUND to
                if (r.sdkInt >= Build.VERSION_CODES.Q) Manifest.permission.ACCESS_BACKGROUND_LOCATION in r.granted else foreground,
            PermissionKeys.MICROPHONE to (Manifest.permission.RECORD_AUDIO in r.granted),
            PermissionKeys.CONTACTS to
                (Manifest.permission.READ_CONTACTS in r.granted && Manifest.permission.WRITE_CONTACTS in r.granted),
            PermissionKeys.SMS to (Manifest.permission.SEND_SMS in r.granted),
            PermissionKeys.BATTERY_UNRESTRICTED to r.ignoringBatteryOptimizations,
            PermissionKeys.FULL_SCREEN_INTENT to r.canUseFullScreenIntent?.takeIf { r.sdkInt >= Build.VERSION_CODES.UPSIDE_DOWN_CAKE },
            PermissionKeys.HIBERNATION_EXEMPT to r.autoRevokeExempt?.takeIf { r.sdkInt >= Build.VERSION_CODES.R },
            PermissionKeys.ACCESSIBILITY_SERVICE to r.accessibilityServiceOn,
        )
        return PermissionKeys.ALL.mapNotNull { name -> values[name]?.let { name to it } }.toMap()
    }

    /** The AlertEvent's device_state, captured at [capturedAtMs] (wall clock). */
    fun deviceState(r: PermissionReadings, capturedAtMs: Long) =
        DeviceState(capturedAt = Instant.ofEpochMilli(capturedAtMs).toString(), permissions = permissions(r))

    /** For the trigger log: "8/10", plus the names that are off. Names only, never a value of the person's. */
    fun summary(permissions: Map<String, Boolean>): String {
        val off = permissions.filterValues { !it }.keys
        val granted = "${permissions.size - off.size}/${permissions.size}"
        return if (off.isEmpty()) granted else "$granted off=${off.joinToString(",")}"
    }
}
