package com.team27.mama.emergency

import kotlinx.coroutines.ExperimentalCoroutinesApi
import kotlinx.coroutines.flow.MutableStateFlow
import kotlinx.coroutines.flow.StateFlow
import kotlinx.coroutines.launch
import kotlinx.coroutines.test.advanceTimeBy
import kotlinx.coroutines.test.currentTime
import kotlinx.coroutines.test.runCurrent
import kotlinx.coroutines.test.runTest
import org.junit.Assert.assertEquals
import org.junit.Assert.assertFalse
import org.junit.Assert.assertTrue
import org.junit.Test

@OptIn(ExperimentalCoroutinesApi::class)
class OfflineFallbackTest {
    private class FakeControls(phase: EmergencyPhase) : EmergencyControls {
        val flow = MutableStateFlow(EmergencyState(phase = phase))
        override val state: StateFlow<EmergencyState> = flow

        override fun declare(): Boolean = false

        override fun cancel() {}
    }

    @Test
    fun declaresOnlyWhenTheAlertDidNotReachTheServer() {
        assertFalse(OfflineFallback.shouldDeclare(200))
        assertFalse(OfflineFallback.shouldDeclare(201))
        assertTrue(OfflineFallback.shouldDeclare(-1))
        assertTrue(OfflineFallback.shouldDeclare(422))
        assertTrue(OfflineFallback.shouldDeclare(503))
        assertTrue(OfflineFallback.shouldDeclare(null))
    }

    @Test
    fun onlyAForcedChannelWithoutHttpSkipsTheOnlineAlert() {
        assertFalse(OfflineFallback.skipsOnlineAlert(ChannelOverride.AUTO, emergencyEnabled = true))
        assertTrue(OfflineFallback.skipsOnlineAlert(ChannelOverride.NO_INTERNET, emergencyEnabled = true))
        assertTrue(OfflineFallback.skipsOnlineAlert(ChannelOverride.SMS_ONLY, emergencyEnabled = true))
        assertTrue(OfflineFallback.skipsOnlineAlert(ChannelOverride.BLE_ONLY, emergencyEnabled = true))
        ChannelOverride.entries.forEach { assertFalse(OfflineFallback.skipsOnlineAlert(it, emergencyEnabled = false)) }
    }

    @Test
    fun noNetworkAtAllSkipsTheOnlineAlertOnlyWithTheFlag() {
        assertTrue(OfflineFallback.skipsOnlineAlert(ChannelOverride.AUTO, emergencyEnabled = true, hasNetwork = false))
        assertFalse(OfflineFallback.skipsOnlineAlert(ChannelOverride.AUTO, emergencyEnabled = true, hasNetwork = true))
        assertFalse(OfflineFallback.skipsOnlineAlert(ChannelOverride.AUTO, emergencyEnabled = false, hasNetwork = false))
    }

    @Test
    fun withTheFlagTheFirstFailedAlertGoesOfflineWithoutRetries() {
        val retries = listOf(1_000L, 2_000L)
        assertEquals(emptyList<Long>(), OfflineFallback.alertRetryDelaysMs(emergencyEnabled = true, retries))
        assertEquals(retries, OfflineFallback.alertRetryDelaysMs(emergencyEnabled = false, retries))
    }

    @Test
    fun holdsWhileDeliveringAndReleasesOnAck() = runTest {
        val controls = FakeControls(EmergencyPhase.SEARCHING_NEARBY)
        var released = false
        launch {
            OfflineFallback.awaitDelivered(controls)
            released = true
        }
        advanceTimeBy(60_000)
        assertFalse(released)
        controls.flow.value = EmergencyState(phase = EmergencyPhase.NO_CHANNEL)
        runCurrent()
        assertFalse(released)
        controls.flow.value = EmergencyState(phase = EmergencyPhase.NOTIFIED)
        runCurrent()
        assertTrue(released)
    }

    @Test
    fun releasesAfterTheCapWithoutAnAck() = runTest {
        OfflineFallback.awaitDelivered(FakeControls(EmergencyPhase.SEARCHING_NEARBY))
        assertEquals(OfflineFallback.HOLD_MAX_MS, currentTime)
    }

    @Test
    fun cancellingReleasesAtOnceSoTheTriggerTakesANewAlert() = runTest {
        OfflineFallback.awaitDelivered(FakeControls(EmergencyPhase.CANCELLING))
        assertEquals(0L, currentTime)
    }

    @Test
    fun cancelledEmergencyReleasesAtOnce() = runTest {
        OfflineFallback.awaitDelivered(FakeControls(EmergencyPhase.CANCELLED))
        assertEquals(0L, currentTime)
    }
}
