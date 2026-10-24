package com.team27.mama.setup

import android.Manifest
import com.team27.mama.setup.SetupStep.ACCESSIBILITY
import com.team27.mama.setup.SetupStep.AUTOSTART
import com.team27.mama.setup.SetupStep.BACKGROUND_LOCATION
import com.team27.mama.setup.SetupStep.BATTERY
import com.team27.mama.setup.SetupStep.CONTACTS
import com.team27.mama.setup.SetupStep.LOCATION
import com.team27.mama.setup.SetupStep.MICROPHONE
import com.team27.mama.setup.SetupStep.NEARBY
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
            SetupStep.forDevice(sdkInt = 36, isXiaomi = true, offlineEmergency = false),
        )
    }

    @Test
    fun otherPhonesSkipTheMiuiScreensAndAccessibilityStaysLast() {
        assertEquals(
            listOf(NOTIFICATIONS, LOCATION, BACKGROUND_LOCATION, MICROPHONE, CONTACTS, SMS, BATTERY, ACCESSIBILITY),
            SetupStep.forDevice(sdkInt = 36, isXiaomi = false, offlineEmergency = false),
        )
    }

    @Test
    fun offlineEmergencyAsksForBluetoothRightAfterSmsOnAndroid12AndLater() {
        assertEquals(
            listOf(NOTIFICATIONS, LOCATION, BACKGROUND_LOCATION, MICROPHONE, CONTACTS, SMS, NEARBY, BATTERY, ACCESSIBILITY),
            SetupStep.forDevice(sdkInt = 36, isXiaomi = false, offlineEmergency = true),
        )
        assertFalse(NEARBY in SetupStep.forDevice(sdkInt = 30, isXiaomi = false, offlineEmergency = true))
        assertTrue(NEARBY.isGrantedBy { true })
        assertFalse(NEARBY.isGrantedBy { it != Manifest.permission.BLUETOOTH_ADVERTISE })
    }

    @Test
    fun olderAndroidSkipsPermissionsItGrantsImplicitly() {
        // API 28: no runtime notification permission, background location comes with foreground.
        assertEquals(
            listOf(LOCATION, MICROPHONE, CONTACTS, SMS, BATTERY, ACCESSIBILITY),
            SetupStep.forDevice(sdkInt = 28, isXiaomi = false, offlineEmergency = false),
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
    fun locationCountsWithApproximateButContactsNeedsReadAndWrite() {
        assertTrue(LOCATION.isGrantedBy { it == Manifest.permission.ACCESS_COARSE_LOCATION })
        assertFalse(CONTACTS.isGrantedBy { it == Manifest.permission.READ_CONTACTS })
        assertTrue(CONTACTS.isGrantedBy { true })
        assertFalse(AUTOSTART.isGrantedBy { true })
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
    fun aFirstNoWithoutRationaleIsOnlyADenialTheSecondBlocks() {
        // A dismissed dialog on Android 11+ (or MIUI) answers "cannot ask again" the first time.
        var flow = PermissionFlow.start(listOf(MICROPHONE))
            .afterRequest(MICROPHONE, granted = false, canAskAgain = false)
        assertEquals(StepStatus.DENIED, flow.statusOf(MICROPHONE))
        flow = flow.afterRequest(MICROPHONE, granted = false, canAskAgain = false)
        assertEquals(StepStatus.BLOCKED, flow.statusOf(MICROPHONE))
    }

    @Test
    fun skippedAndVisitedStepsSurviveARestartButDenialsDoNot() {
        val before = PermissionFlow.start(listOf(LOCATION, SMS, AUTOSTART, BATTERY))
            .afterRequest(LOCATION, granted = false, canAskAgain = true)
            .skip(SMS)
            .afterVisit(AUTOSTART)
        assertEquals(mapOf(SMS to StepStatus.SKIPPED, AUTOSTART to StepStatus.VISITED), before.settled)

        // AUTOSTART only exists on Xiaomi: a saved status for a step this phone lacks is dropped.
        val after = PermissionFlow.start(listOf(LOCATION, SMS, BATTERY)).restored(before.settled)
        assertEquals(StepStatus.PENDING, after.statusOf(LOCATION))
        assertEquals(StepStatus.SKIPPED, after.statusOf(SMS))
        assertEquals(setOf(LOCATION, SMS, BATTERY), after.statuses.keys)
        assertEquals(LOCATION, after.current)
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

    @Test
    fun aVisitedOrSkippedStepCanBeOpenedAgain() {
        val flow = PermissionFlow.start(listOf(LOCATION, BATTERY, ACCESSIBILITY))
            .afterVisit(ACCESSIBILITY)
            .skip(BATTERY)
            .afterRequest(LOCATION, granted = true, canAskAgain = true)
        assertFalse(flow.canReopen(LOCATION))
        assertTrue(flow.canReopen(BATTERY))
        assertTrue(flow.canReopen(ACCESSIBILITY))
        assertEquals(StepAction.SYSTEM_SCREEN, flow.actionFor(ACCESSIBILITY))
        assertEquals(StepAction.SYSTEM_SCREEN, flow.actionFor(BATTERY))
    }

    @Test
    fun aDeniedPermissionAsksAgainAndABlockedOneOpensAppSettings() {
        var flow = PermissionFlow.start(listOf(MICROPHONE))
            .afterRequest(MICROPHONE, granted = false, canAskAgain = true)
        assertEquals(StepAction.REQUEST, flow.actionFor(MICROPHONE))
        flow = flow.afterRequest(MICROPHONE, granted = false, canAskAgain = false)
        assertEquals(StepAction.APP_SETTINGS, flow.actionFor(MICROPHONE))
    }

    @Test
    fun askingAgainAfterSkippingABlockedPermissionBlocksAtOnceWhenTheSystemRefuses() {
        var flow = PermissionFlow.start(listOf(MICROPHONE, CONTACTS))
            .afterRequest(MICROPHONE, granted = false, canAskAgain = true)
            .afterRequest(MICROPHONE, granted = false, canAskAgain = false)
            .skip(MICROPHONE)
        assertEquals(StepAction.REQUEST, flow.actionFor(MICROPHONE))
        flow = flow.afterRequest(MICROPHONE, granted = false, canAskAgain = false)
        assertEquals(StepStatus.BLOCKED, flow.statusOf(MICROPHONE))
        assertEquals(StepAction.APP_SETTINGS, flow.actionFor(MICROPHONE))
    }
}
