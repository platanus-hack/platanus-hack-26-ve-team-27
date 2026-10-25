package com.team27.mama.ui.home

import com.team27.mama.trigger.TriggerProgress
import org.junit.Assert.assertEquals
import org.junit.Assert.assertNull
import org.junit.Test

class HomeModelTest {
    private var requests = 0
    private val model = HomeModel { requests++ }
    private var now = 10_000L

    private fun advance(ms: Long, progress: TriggerProgress = TriggerProgress.Idle) {
        now += ms
        model.update(now, progress)
    }

    private fun hold() {
        model.pressStart(now)
        advance(HomeModel.HOLD_MS)
    }

    private val phase get() = model.state.phase

    @Test
    fun startsIdle() {
        assertEquals(HomePhase.Idle, phase)
        assertEquals(0, requests)
    }

    @Test
    fun holdingFillsTheRingUntilTheTriggerIsRequested() {
        model.pressStart(now)
        advance(750)
        assertEquals(HomePhase.Arming, phase)
        assertEquals(0.5f, model.state.armProgress, 0.001f)
        advance(750)
        assertEquals(HomePhase.Processing, phase)
        assertEquals(1, requests)
    }

    @Test
    fun releasingEarlyReturnsToIdleWithoutRequesting() {
        model.pressStart(now)
        advance(1_000)
        model.pressEnd()
        advance(1_000)
        assertEquals(HomePhase.Idle, phase)
        assertEquals(0f, model.state.armProgress, 0f)
        assertEquals(0, requests)
    }

    @Test
    fun releasingAfterTheHoldDoesNotCancelOrRepeatTheRequest() {
        hold()
        model.pressEnd()
        advance(100, TriggerProgress.Sending)
        assertEquals(HomePhase.Processing, phase)
        assertEquals(1, requests)
    }

    @Test
    fun aNewPressWhileProcessingIsIgnored() {
        hold()
        model.pressStart(now)
        advance(2_000, TriggerProgress.Sending)
        assertEquals(1, requests)
        assertEquals(HomePhase.Processing, phase)
    }

    @Test
    fun processingStaysForItsMinimumTimeEvenIfTheAlertIsAlreadyActive() {
        hold()
        advance(100, TriggerProgress.Active)
        assertEquals(HomePhase.Processing, phase)
        advance(HomeModel.MIN_PROCESSING_MS - 100 - 1, TriggerProgress.Active)
        assertEquals(HomePhase.Processing, phase)
        advance(1, TriggerProgress.Active)
        assertEquals(HomePhase.Listening, phase)
    }

    @Test
    fun processingWaitsForTheAlertAfterTheMinimumTime() {
        hold()
        advance(3_000, TriggerProgress.Sending)
        assertEquals(HomePhase.Processing, phase)
        advance(100, TriggerProgress.Active)
        assertEquals(HomePhase.Listening, phase)
    }

    @Test
    fun listeningCountsSecondsFromItsStart() {
        hold()
        advance(HomeModel.MIN_PROCESSING_MS, TriggerProgress.Active)
        assertEquals(0, model.state.listeningSeconds)
        advance(5_400, TriggerProgress.Active)
        assertEquals(5, model.state.listeningSeconds)
    }

    @Test
    fun anIncidentThatEndsWhileListeningReturnsToIdle() {
        hold()
        advance(HomeModel.MIN_PROCESSING_MS, TriggerProgress.Active)
        advance(500, TriggerProgress.Idle)
        assertEquals(HomeState(), model.state)
    }

    @Test
    fun aServiceThatStaysIdleFailsAfterTheTimeout() {
        hold()
        advance(HomeModel.START_TIMEOUT_MS - 1)
        assertEquals(HomePhase.Processing, phase)
        advance(1)
        assertEquals(HomePhase.Failed, phase)
        assertEquals(HomeFailure.NotStarted, model.state.failure)
    }

    @Test
    fun aSendingThatEndsWithoutBeingAcceptedFails() {
        hold()
        advance(500, TriggerProgress.Sending)
        advance(2_000, TriggerProgress.Idle)
        assertEquals(HomePhase.Failed, phase)
        assertEquals(HomeFailure.NotDelivered, model.state.failure)
    }

    @Test
    fun failedReturnsToIdleAfterAMomentAndClearsTheReason() {
        hold()
        advance(HomeModel.START_TIMEOUT_MS)
        advance(HomeModel.FAILED_MS - 1)
        assertEquals(HomePhase.Failed, phase)
        advance(1)
        assertEquals(HomePhase.Idle, phase)
        assertNull(model.state.failure)
    }

    @Test
    fun cancellingWhileProcessingShowsCancelledThenIdle() {
        hold()
        advance(300, TriggerProgress.Sending)
        model.cancel(now)
        assertEquals(HomePhase.Cancelled, phase)
        advance(HomeModel.CANCELLED_MS - 1)
        assertEquals(HomePhase.Cancelled, phase)
        advance(1)
        assertEquals(HomePhase.Idle, phase)
    }

    @Test
    fun cancellingWhileListeningShowsCancelled() {
        hold()
        advance(HomeModel.MIN_PROCESSING_MS, TriggerProgress.Active)
        model.cancel(now)
        assertEquals(HomePhase.Cancelled, phase)
        assertEquals(0, model.state.listeningSeconds)
    }

    @Test
    fun cancellingWhenNothingIsShownDoesNothing() {
        model.cancel(now)
        assertEquals(HomePhase.Idle, phase)
    }

    @Test
    fun aSecondHoldAfterCancellingRequestsAgainOnce() {
        hold()
        model.cancel(now)
        advance(HomeModel.CANCELLED_MS)
        hold()
        assertEquals(2, requests)
        assertEquals(HomePhase.Processing, phase)
    }

    @Test
    fun anActiveProgressAtTheRequestDoesNotSkipTheMinimumTime() {
        hold()
        advance(0, TriggerProgress.Active)
        assertEquals(HomePhase.Processing, phase)
    }

    @Test
    fun theAccessibilityActionRequestsTheTriggerWithoutHolding() {
        model.activate(now)
        assertEquals(HomePhase.Processing, phase)
        assertEquals(1, requests)
        model.activate(now)
        assertEquals(1, requests)
    }

    @Test
    fun theStepBarsSeeTheLatestProgress() {
        hold()
        advance(100, TriggerProgress.Sending)
        assertEquals(TriggerProgress.Sending, model.state.progress)
    }

    @Test
    fun anIncidentStartedElsewhereIsShownAsProcessingOrListening() {
        advance(100, TriggerProgress.Sending)
        assertEquals(HomePhase.Processing, phase)
        assertEquals(0, requests)
        advance(HomeModel.MIN_PROCESSING_MS, TriggerProgress.Active)
        assertEquals(HomePhase.Listening, phase)
    }

    @Test
    fun anActiveIncidentFoundOnCreationIsListening() {
        advance(100, TriggerProgress.Active)
        assertEquals(HomePhase.Listening, phase)
        assertEquals(0, requests)
    }

    @Test
    fun aCancelledIncidentIsNotAdoptedAgainWhileTheServiceIsStillWindingDown() {
        hold()
        advance(300, TriggerProgress.Sending)
        model.cancel(now)
        advance(HomeModel.CANCELLED_MS, TriggerProgress.Sending)
        advance(500, TriggerProgress.Active)
        assertEquals(HomePhase.Idle, phase)
        advance(500, TriggerProgress.Idle)
        advance(500, TriggerProgress.Sending)
        assertEquals(HomePhase.Processing, phase)
    }

    @Test
    fun anAdoptedIncidentDoesNotInheritTheActiveFlagOfAnEarlierOne() {
        hold()
        advance(HomeModel.MIN_PROCESSING_MS, TriggerProgress.Active)
        advance(500, TriggerProgress.Idle)
        advance(100, TriggerProgress.Sending)
        assertEquals(HomePhase.Processing, phase)
        advance(HomeModel.MIN_PROCESSING_MS, TriggerProgress.Idle)
        assertEquals(HomePhase.Failed, phase)
        assertEquals(HomeFailure.NotDelivered, model.state.failure)
    }

    @Test
    fun formatsTheElapsedTimeAsMinutesAndSeconds() {
        assertEquals("00:00", HomeModel.formatElapsed(0))
        assertEquals("00:59", HomeModel.formatElapsed(59))
        assertEquals("01:05", HomeModel.formatElapsed(65))
        assertEquals("12:00", HomeModel.formatElapsed(720))
    }
}
