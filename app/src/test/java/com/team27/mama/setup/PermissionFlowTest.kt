package com.team27.mama.setup

import android.Manifest
import com.team27.mama.setup.SetupStep.ACCESSIBILITY
import com.team27.mama.setup.SetupStep.AUTOSTART
import com.team27.mama.setup.SetupStep.BACKGROUND_LOCATION
import com.team27.mama.setup.SetupStep.BATTERY
import com.team27.mama.setup.SetupStep.CONTACTS
import com.team27.mama.setup.SetupStep.LOCATION
import com.team27.mama.setup.SetupStep.MICROPHONE
import com.team27.mama.setup.SetupStep.NOTIFICATIONS
import com.team27.mama.setup.SetupStep.POPUPS
import com.team27.mama.setup.SetupStep.SMS
import org.junit.Assert.assertEquals
import org.junit.Assert.assertFalse
import org.junit.Assert.assertNull
import org.junit.Assert.assertTrue
import org.junit.Test

class PermissionFlowTest {

    @Test
    fun xiaomiOnRecentAndroidGetsEveryStepInProductOrder() {
        assertEquals(
            listOf(NOTIFICATIONS, LOCATION, BACKGROUND_LOCATION, MICROPHONE, CONTACTS, SMS, AUTOSTART, BATTERY, POPUPS, ACCESSIBILITY),
            SetupStep.forDevice(sdkInt = 36, isXiaomi = true),
        )
    }

    @Test
    fun otherPhonesSkipTheMiuiScreensAndAccessibilityStaysLast() {
        assertEquals(
            listOf(NOTIFICATIONS, LOCATION, BACKGROUND_LOCATION, MICROPHONE, CONTACTS, SMS, BATTERY, ACCESSIBILITY),
            SetupStep.forDevice(sdkInt = 36, isXiaomi = false),
        )
    }

    @Test
    fun olderAndroidSkipsPermissionsItGrantsImplicitly() {
        // API 28: no runtime notification permission, background location comes with foreground.
        assertEquals(
            listOf(LOCATION, MICROPHONE, CONTACTS, SMS, BATTERY, ACCESSIBILITY),
            SetupStep.forDevice(sdkInt = 28, isXiaomi = false),
        )
        assertTrue(BACKGROUND_LOCATION in SetupStep.forDevice(sdkInt = 29, isXiaomi = false))
        assertFalse(NOTIFICATIONS in SetupStep.forDevice(sdkInt = 32, isXiaomi = false))
        assertTrue(NOTIFICATIONS in SetupStep.forDevice(sdkInt = 33, isXiaomi = false))
    }

    @Test
    fun backgroundLocationIsRequestedOnItsOwnRightAfterForeground() {
        val steps = SetupStep.forDevice(sdkInt = 36, isXiaomi = false)
        assertEquals(steps.indexOf(LOCATION) + 1, steps.indexOf(BACKGROUND_LOCATION))
        assertEquals(listOf(Manifest.permission.ACCESS_BACKGROUND_LOCATION), BACKGROUND_LOCATION.permissions)
    }

    @Test
    fun currentStepIsTheFirstStillBlocking() {
        var flow = PermissionFlow.start(SetupStep.forDevice(sdkInt = 36, isXiaomi = false))
        assertEquals(NOTIFICATIONS, flow.current)
        flow = flow.afterRequest(NOTIFICATIONS, granted = true, canAskAgain = true)
        assertEquals(LOCATION, flow.current)
    }

    @Test
    fun aDenialStaysCurrentUntilRetriedOrSkipped() {
        var flow = PermissionFlow.start(listOf(LOCATION, MICROPHONE))
        flow = flow.afterRequest(LOCATION, granted = false, canAskAgain = true)
        assertEquals(StepStatus.DENIED, flow.statusOf(LOCATION))
        assertEquals(LOCATION, flow.current)

        flow = flow.afterRequest(LOCATION, granted = false, canAskAgain = false)
        assertEquals(StepStatus.BLOCKED, flow.statusOf(LOCATION))
        assertEquals(LOCATION, flow.current)

        flow = flow.skip(LOCATION)
        assertEquals(StepStatus.SKIPPED, flow.statusOf(LOCATION))
        assertEquals(MICROPHONE, flow.current)
    }

    @Test
    fun retryAfterADenialCanStillGrant() {
        val flow = PermissionFlow.start(listOf(CONTACTS))
            .afterRequest(CONTACTS, granted = false, canAskAgain = true)
            .afterRequest(CONTACTS, granted = true, canAskAgain = true)
        assertEquals(StepStatus.GRANTED, flow.statusOf(CONTACTS))
        assertTrue(flow.isFinished)
    }

    @Test
    fun visitedSystemScreensCountAsDoneButNeverDowngradeAGrant() {
        var flow = PermissionFlow.start(listOf(AUTOSTART, BATTERY))
        flow = flow.afterVisit(AUTOSTART)
        assertEquals(StepStatus.VISITED, flow.statusOf(AUTOSTART))
        flow = flow.refreshed { if (it == BATTERY) true else null }.afterVisit(BATTERY)
        assertEquals(StepStatus.GRANTED, flow.statusOf(BATTERY))
        assertTrue(flow.isFinished)
        assertNull(flow.current)
    }

    @Test
    fun refreshPicksUpGrantsFromSettingsAndRevocations() {
        var flow = PermissionFlow.start(listOf(LOCATION, MICROPHONE, AUTOSTART))
            .afterRequest(LOCATION, granted = false, canAskAgain = false)
            .afterRequest(MICROPHONE, granted = true, canAskAgain = true)
            .afterVisit(AUTOSTART)
        flow = flow.refreshed { step ->
            when (step) {
                LOCATION -> true // granted from the app settings screen
                MICROPHONE -> false // revoked meanwhile
                else -> null // MIUI screens cannot be read back
            }
        }
        assertEquals(StepStatus.GRANTED, flow.statusOf(LOCATION))
        assertEquals(StepStatus.PENDING, flow.statusOf(MICROPHONE))
        assertEquals(StepStatus.VISITED, flow.statusOf(AUTOSTART))
        assertEquals(MICROPHONE, flow.current)
    }

    @Test
    fun refreshKeepsADenialThatIsStillNotGranted() {
        val flow = PermissionFlow.start(listOf(SMS))
            .afterRequest(SMS, granted = false, canAskAgain = true)
            .refreshed { false }
        assertEquals(StepStatus.DENIED, flow.statusOf(SMS))
    }
}
