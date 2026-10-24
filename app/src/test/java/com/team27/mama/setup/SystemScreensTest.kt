package com.team27.mama.setup

import android.provider.Settings
import org.junit.Assert.assertEquals
import org.junit.Assert.assertFalse
import org.junit.Assert.assertTrue
import org.junit.Test

class SystemScreensTest {
    private val pkg = "com.team27.mama"

    private fun targets(step: SetupStep, xiaomi: Boolean) =
        SystemScreens.targetsFor(step, appPackage = pkg, appLabel = "MAMÁ", isXiaomi = xiaomi)

    @Test
    fun everySystemStepFallsBackToAppSettings() {
        SetupStep.entries.filter { it.isSystemScreen }.forEach { step ->
            listOf(true, false).forEach { xiaomi ->
                val list = targets(step, xiaomi)
                assertEquals("$step xiaomi=$xiaomi", SystemScreens.appSettings, list.last())
                assertTrue("$step has its own screen first", list.size >= 2)
            }
        }
    }

    @Test
    fun xiaomiScreensPointAtMiuiSecurityCenterWithOurPackage() {
        val autostart = targets(SetupStep.AUTOSTART, xiaomi = true).first()
        assertEquals("com.miui.securitycenter", autostart.packageName)
        assertEquals("com.miui.permcenter.autostart.AutoStartManagementActivity", autostart.className)

        val popups = targets(SetupStep.POPUPS, xiaomi = true).first()
        assertEquals("miui.intent.action.APP_PERM_EDITOR", popups.action)
        assertEquals(pkg, popups.extras["extra_pkgname"])
    }

    @Test
    fun batteryTriesMiuiOnlyOnXiaomiThenTheAndroidExemption() {
        val xiaomi = targets(SetupStep.BATTERY, xiaomi = true)
        assertEquals("com.miui.powerkeeper", xiaomi.first().packageName)
        assertEquals(pkg, xiaomi.first().extras["package_name"])
        assertEquals(Settings.ACTION_REQUEST_IGNORE_BATTERY_OPTIMIZATIONS, xiaomi[1].action)
        assertTrue(xiaomi[1].withPackageUri)

        val other = targets(SetupStep.BATTERY, xiaomi = false)
        assertFalse(other.any { it.packageName?.startsWith("com.miui") == true })
        assertEquals(Settings.ACTION_REQUEST_IGNORE_BATTERY_OPTIMIZATIONS, other.first().action)
    }

    @Test
    fun accessibilityOpensTheAccessibilitySettings() {
        assertEquals(Settings.ACTION_ACCESSIBILITY_SETTINGS, targets(SetupStep.ACCESSIBILITY, xiaomi = true).first().action)
    }

    @Test
    fun recognizesXiaomiBrands() {
        assertTrue(SystemScreens.isXiaomi(manufacturer = "Xiaomi", brand = "Redmi"))
        assertTrue(SystemScreens.isXiaomi(manufacturer = "XIAOMI", brand = "xiaomi"))
        assertTrue(SystemScreens.isXiaomi(manufacturer = "unknown", brand = "POCO"))
        assertFalse(SystemScreens.isXiaomi(manufacturer = "Google", brand = "google"))
    }
}
