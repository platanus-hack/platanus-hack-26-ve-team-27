package com.team27.mama.relay

import android.Manifest.permission as P
import com.team27.mama.emergency.EmergencyPhase
import kotlinx.coroutines.ExperimentalCoroutinesApi
import kotlinx.coroutines.flow.MutableStateFlow
import kotlinx.coroutines.test.TestScope
import kotlinx.coroutines.test.runCurrent
import kotlinx.coroutines.test.runTest
import org.junit.Assert.assertEquals
import org.junit.Assert.assertFalse
import org.junit.Assert.assertTrue
import org.junit.Test

@OptIn(ExperimentalCoroutinesApi::class)
class MeshPolicyTest {
    @Test
    fun meshModeTable() {
        for (visible in listOf(false, true)) for (relay in listOf(false, true)) for (emergency in listOf(false, true)) {
            val expected = when {
                relay || emergency -> MeshMode.SERVICE
                visible -> MeshMode.APP_ONLY
                else -> MeshMode.OFF
            }
            assertEquals(expected, meshMode(visible, relay, emergency))
        }
        assertEquals(MeshMode.OFF, meshMode(false, false, false))
        assertEquals(MeshMode.APP_ONLY, meshMode(true, false, false))
    }

    @Test
    fun keepsMeshOnPhases() {
        for (p in EmergencyPhase.entries) {
            val off = p == EmergencyPhase.IDLE || p == EmergencyPhase.CANCELLED
            assertEquals(p.name, !off, p.keepsMeshOn())
        }
    }

    private class Rig(scope: TestScope) {
        val visible = MutableStateFlow(false)
        val relay = MutableStateFlow(false)
        val emergency = MutableStateFlow(false)
        val calls = mutableListOf<String>()
        val controller = MeshController(
            visible,
            relay,
            emergency,
            startGateway = { calls += "startGateway" },
            stopGateway = { calls += "stopGateway" },
            startService = { calls += "startService" },
            stopService = { calls += "stopService" },
            scope = scope.backgroundScope,
        )

        fun take(): List<String> = calls.toList().also { calls.clear() }
    }

    @Test
    fun controllerAppliesModes() = runTest {
        val r = Rig(this)
        r.controller.start()
        runCurrent()
        assertEquals(listOf("stopService", "stopGateway"), r.take())
        r.visible.value = true
        runCurrent()
        assertEquals(listOf("stopService", "startGateway"), r.take())
        r.relay.value = true
        runCurrent()
        assertEquals(listOf("startService", "startGateway"), r.take())
        r.visible.value = false
        runCurrent()
        assertEquals(listOf("startService", "startGateway"), r.take())
    }

    @Test
    fun reappliesOnEveryInputChange() = runTest {
        val r = Rig(this)
        r.relay.value = true
        r.controller.start()
        runCurrent()
        r.visible.value = true
        runCurrent()
        r.visible.value = false
        runCurrent()
        assertEquals(3, r.calls.count { it == "startService" })
    }

    @Test
    fun permissions() {
        assertEquals(listOf(P.ACCESS_FINE_LOCATION, P.ACCESS_COARSE_LOCATION), MeshPermissions.required(30))
        assertEquals(
            listOf(
                P.BLUETOOTH_SCAN,
                P.BLUETOOTH_ADVERTISE,
                P.BLUETOOTH_CONNECT,
                P.ACCESS_FINE_LOCATION,
                P.ACCESS_COARSE_LOCATION,
            ),
            MeshPermissions.required(31),
        )
        assertEquals(MeshPermissions.required(32), MeshPermissions.requested(32))
        assertEquals(MeshPermissions.required(33) + P.POST_NOTIFICATIONS, MeshPermissions.requested(33))
        assertFalse(P.POST_NOTIFICATIONS in MeshPermissions.requested(30))
    }

    @Test
    fun satisfied() {
        val ble = setOf(P.BLUETOOTH_SCAN, P.BLUETOOTH_ADVERTISE, P.BLUETOOTH_CONNECT)
        assertTrue(MeshPermissions.satisfied(31) { it in ble + P.ACCESS_COARSE_LOCATION })
        assertTrue(MeshPermissions.satisfied(31) { it in ble + P.ACCESS_FINE_LOCATION })
        assertFalse(MeshPermissions.satisfied(31) { it in ble })
        assertFalse(MeshPermissions.satisfied(31) { it == P.ACCESS_FINE_LOCATION })
        assertTrue(MeshPermissions.satisfied(30) { it == P.ACCESS_COARSE_LOCATION })
        assertFalse(MeshPermissions.satisfied(30) { false })
    }
}
