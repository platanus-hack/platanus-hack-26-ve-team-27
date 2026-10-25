package com.team27.mama.permissions

import android.Manifest
import android.os.Build
import com.team27.mama.contract.PermissionKeys
import org.junit.Assert.assertEquals
import org.junit.Assert.assertFalse
import org.junit.Assert.assertTrue
import org.junit.Test

class PermissionSnapshotTest {

    private val everyRuntime = setOf(
        Manifest.permission.ACCESS_FINE_LOCATION,
        Manifest.permission.ACCESS_COARSE_LOCATION,
        Manifest.permission.ACCESS_BACKGROUND_LOCATION,
        Manifest.permission.RECORD_AUDIO,
        Manifest.permission.READ_CONTACTS,
        Manifest.permission.WRITE_CONTACTS,
        Manifest.permission.SEND_SMS,
    )

    private fun readings(
        sdkInt: Int = Build.VERSION_CODES.UPSIDE_DOWN_CAKE,
        granted: Set<String> = everyRuntime,
        fullScreen: Boolean? = true,
        autoRevokeExempt: Boolean? = true,
    ) = PermissionReadings(
        sdkInt = sdkInt,
        granted = granted,
        notificationsEnabled = true,
        accessibilityServiceOn = true,
        ignoringBatteryOptimizations = true,
        canUseFullScreenIntent = fullScreen,
        autoRevokeExempt = autoRevokeExempt,
    )

    @Test
    fun anAndroid14PhoneWithEverythingHasAllTenGranted() {
        val p = PermissionSnapshot.permissions(readings())
        assertEquals(PermissionKeys.ALL, p.keys.toList())
        assertTrue(p.values.all { it })
        assertEquals("10/10", PermissionSnapshot.summary(p))
    }

    @Test
    fun beforeAndroid10BackgroundLocationComesWithTheForegroundOne() {
        val coarseOnly = readings(sdkInt = Build.VERSION_CODES.P, granted = setOf(Manifest.permission.ACCESS_COARSE_LOCATION))
        val p = PermissionSnapshot.permissions(coarseOnly)
        assertFalse("approximate is not precise", p.getValue(PermissionKeys.LOCATION_FINE))
        assertTrue(p.getValue(PermissionKeys.LOCATION_BACKGROUND))
        // From Android 10 it is its own permission.
        val q = PermissionSnapshot.permissions(readings(sdkInt = Build.VERSION_CODES.Q, granted = setOf(Manifest.permission.ACCESS_FINE_LOCATION)))
        assertTrue(q.getValue(PermissionKeys.LOCATION_FINE))
        assertFalse(q.getValue(PermissionKeys.LOCATION_BACKGROUND))
    }

    @Test
    fun permissionsTheAndroidVersionLacksAreLeftOut() {
        val android10 = PermissionSnapshot.permissions(readings(sdkInt = Build.VERSION_CODES.Q, fullScreen = null, autoRevokeExempt = null))
        assertFalse(PermissionKeys.FULL_SCREEN_INTENT in android10)
        assertFalse(PermissionKeys.HIBERNATION_EXEMPT in android10)
        assertEquals(8, android10.size)
        // A value read on a version that cannot have it is ignored too.
        val android13 = PermissionSnapshot.permissions(readings(sdkInt = Build.VERSION_CODES.TIRAMISU, fullScreen = false))
        assertFalse(PermissionKeys.FULL_SCREEN_INTENT in android13)
        assertTrue(android13.getValue(PermissionKeys.HIBERNATION_EXEMPT))
    }

    @Test
    fun contactsNeedsReadAndWrite() {
        val readOnly = everyRuntime - Manifest.permission.WRITE_CONTACTS
        assertFalse(PermissionSnapshot.permissions(readings(granted = readOnly)).getValue(PermissionKeys.CONTACTS))
    }

    @Test
    fun deviceStateStampsTheCaptureTime() {
        val state = PermissionSnapshot.deviceState(readings(granted = emptySet()), 1_761_296_400_251)
        assertEquals("2025-10-24T09:00:00.251Z", state.capturedAt)
        assertFalse(state.permissions.getValue(PermissionKeys.MICROPHONE))
        assertEquals(
            "5/10 off=location_fine,location_background,microphone,contacts,sms",
            PermissionSnapshot.summary(state.permissions),
        )
    }
}
