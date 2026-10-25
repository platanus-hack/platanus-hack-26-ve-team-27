package com.team27.mama.permissions

import android.Manifest
import android.app.NotificationManager
import android.content.Context
import android.content.pm.PackageManager
import android.os.Build
import android.os.PowerManager
import android.provider.Settings
import androidx.core.app.NotificationManagerCompat
import androidx.core.content.ContextCompat

/**
 * Reads the phone's permissions for #125. Every read is a quick local query
 * and none asks the person for anything, so the trigger can call it at the
 * gesture. Not unit-tested (Android); the rules live in [PermissionSnapshot].
 */
class AndroidPermissions(context: Context) {
    private val app = context.applicationContext

    fun read(): PermissionReadings = PermissionReadings(
        sdkInt = Build.VERSION.SDK_INT,
        granted = RUNTIME.filterTo(mutableSetOf()) {
            ContextCompat.checkSelfPermission(app, it) == PackageManager.PERMISSION_GRANTED
        },
        notificationsEnabled = NotificationManagerCompat.from(app).areNotificationsEnabled(),
        accessibilityServiceOn = accessibilityServiceOn(),
        ignoringBatteryOptimizations = ignoringBatteryOptimizations() ?: false,
        canUseFullScreenIntent = if (Build.VERSION.SDK_INT >= Build.VERSION_CODES.UPSIDE_DOWN_CAKE) {
            app.getSystemService(NotificationManager::class.java)?.canUseFullScreenIntent()
        } else {
            null
        },
        autoRevokeExempt = if (Build.VERSION.SDK_INT >= Build.VERSION_CODES.R) app.packageManager.isAutoRevokeWhitelisted else null,
    )

    /** [read] as the contract's permission map. */
    fun snapshot(): Map<String, Boolean> = PermissionSnapshot.permissions(read())

    /** One of the app's accessibility services (the volume gesture's) is on. Setup's ACCESSIBILITY step reads this. */
    fun accessibilityServiceOn(): Boolean =
        Settings.Secure.getString(app.contentResolver, Settings.Secure.ENABLED_ACCESSIBILITY_SERVICES)
            ?.split(':')
            ?.any { it.startsWith(app.packageName + "/") }
            ?: false

    /** Battery optimization is off for the app; null without a PowerManager. Setup's BATTERY step reads this. */
    fun ignoringBatteryOptimizations(): Boolean? =
        app.getSystemService(PowerManager::class.java)?.isIgnoringBatteryOptimizations(app.packageName)

    private companion object {
        /** The runtime permissions the snapshot looks at. */
        val RUNTIME = listOf(
            Manifest.permission.ACCESS_FINE_LOCATION,
            Manifest.permission.ACCESS_COARSE_LOCATION,
            Manifest.permission.ACCESS_BACKGROUND_LOCATION,
            Manifest.permission.RECORD_AUDIO,
            Manifest.permission.READ_CONTACTS,
            Manifest.permission.WRITE_CONTACTS,
            Manifest.permission.SEND_SMS,
        )
    }
}
