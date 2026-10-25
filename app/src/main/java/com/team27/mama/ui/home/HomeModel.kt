package com.team27.mama.ui.home

import com.team27.mama.trigger.TriggerProgress

/** What the home screen shows. */
enum class HomePhase { Idle, Arming, Processing, Listening, Cancelled, Failed }

/** Why the call did not start. The screen words each one. */
enum class HomeFailure {
    /** The service never started the incident (setup incomplete, location permission missing, one already running). */
    NotStarted,

    /** The incident started but the alert never got through. */
    NotDelivered,
}

/**
 * Everything the screen draws. [armProgress] runs 0..1 while the portrait is held,
 * [listeningSeconds] counts from the moment the phase became [HomePhase.Listening], and [progress]
 * is the last trigger progress the model saw (the step bars read it).
 */
data class HomeState(
    val phase: HomePhase = HomePhase.Idle,
    val armProgress: Float = 0f,
    val listeningSeconds: Int = 0,
    val progress: TriggerProgress = TriggerProgress.Idle,
    val failure: HomeFailure? = null,
)

/**
 * The home screen's logic, with no Android in it: time comes in as `nowMs` (any monotonic clock)
 * and the trigger progress as an argument, so a test drives it with a fake clock.
 *
 * Hold the portrait for [HOLD_MS] and [requestTrigger] runs once. Processing lasts at least
 * [MIN_PROCESSING_MS] so the animation reads even when the service answers at once. When the
 * service stays idle for [START_TIMEOUT_MS] after the request, or ends having only sent, the
 * phase is [HomePhase.Failed]. An incident started elsewhere is adopted (see [adopt]). Cancelled and Failed show for a moment, then the screen is idle.
 *
 * Not thread safe: call it from one thread (the UI's).
 */
class HomeModel(private val requestTrigger: () -> Unit) {
    var state: HomeState = HomeState()
        private set

    private var armedAt = 0L
    private var requestedAt = 0L
    private var listeningAt = 0L
    private var endedAt = 0L
    private var sawStarted = false
    private var sawActive = false

    /** Set by a cancel: the service may still report the cancelled incident, so do not adopt it until it reports idle. */
    private var cancelled = false

    /** The finger went down on the portrait. */
    fun pressStart(nowMs: Long) {
        if (state.phase != HomePhase.Idle) return
        armedAt = nowMs
        state = state.copy(phase = HomePhase.Arming, armProgress = 0f)
    }

    /** The finger came up before the hold finished: back to idle. A release after the hold completed does nothing. */
    fun pressEnd() {
        if (state.phase == HomePhase.Arming) state = state.copy(phase = HomePhase.Idle, armProgress = 0f)
    }

    /** The accessibility action for the hold: skips the arming and requests the trigger now. */
    fun activate(nowMs: Long) {
        if (state.phase != HomePhase.Idle && state.phase != HomePhase.Arming) return
        request(nowMs)
    }

    /** "Fue sin querer". Only means something while an incident is shown. */
    fun cancel(nowMs: Long) {
        if (state.phase != HomePhase.Processing && state.phase != HomePhase.Listening) return
        endedAt = nowMs
        cancelled = true
        state = state.copy(phase = HomePhase.Cancelled, armProgress = 0f, listeningSeconds = 0)
    }

    /** Advances the clock with the service's current [progress]. Call it a few times a second, or each frame while arming. */
    fun update(nowMs: Long, progress: TriggerProgress) {
        if (progress != TriggerProgress.Idle) sawStarted = true
        if (progress == TriggerProgress.Active) sawActive = true
        state = state.copy(progress = progress)
        when (state.phase) {
            HomePhase.Idle -> when {
                cancelled -> if (progress == TriggerProgress.Idle) cancelled = false
                else -> adopt(nowMs, progress)
            }
            HomePhase.Arming -> {
                val fraction = ((nowMs - armedAt).toFloat() / HOLD_MS).coerceIn(0f, 1f)
                if (fraction >= 1f) request(nowMs) else state = state.copy(armProgress = fraction)
            }
            HomePhase.Processing -> processing(nowMs, progress)
            HomePhase.Listening -> {
                state = if (progress == TriggerProgress.Idle) {
                    HomeState()
                } else {
                    state.copy(listeningSeconds = ((nowMs - listeningAt) / 1000).toInt())
                }
            }
            HomePhase.Cancelled -> if (nowMs - endedAt >= CANCELLED_MS) state = HomeState()
            HomePhase.Failed -> if (nowMs - endedAt >= FAILED_MS) state = HomeState()
        }
    }

    /**
     * An incident this model did not request is running (the screen was recreated, or the widget
     * or the volume pattern started it): show it instead of a ready screen.
     */
    private fun adopt(nowMs: Long, progress: TriggerProgress) {
        when (progress) {
            TriggerProgress.Idle -> Unit
            TriggerProgress.Sending -> {
                requestedAt = nowMs
                sawStarted = true
                sawActive = false
                state = state.copy(phase = HomePhase.Processing, armProgress = 1f)
            }
            TriggerProgress.Active -> {
                listeningAt = nowMs
                sawStarted = true
                sawActive = true
                state = state.copy(phase = HomePhase.Listening, armProgress = 1f, listeningSeconds = 0)
            }
        }
    }

    private fun request(nowMs: Long) {
        requestedAt = nowMs
        cancelled = false
        sawStarted = false
        sawActive = false
        state = state.copy(phase = HomePhase.Processing, armProgress = 1f, listeningSeconds = 0, failure = null)
        requestTrigger()
    }

    private fun processing(nowMs: Long, progress: TriggerProgress) {
        val shown = nowMs - requestedAt
        when {
            // The service ignored the start: it never left idle.
            !sawStarted && shown >= START_TIMEOUT_MS -> fail(nowMs, HomeFailure.NotStarted)
            // The service ended again: after an accepted alert the incident is simply over.
            sawStarted && progress == TriggerProgress.Idle && shown >= MIN_PROCESSING_MS ->
                if (sawActive) state = HomeState() else fail(nowMs, HomeFailure.NotDelivered)
            progress == TriggerProgress.Active && shown >= MIN_PROCESSING_MS -> {
                listeningAt = nowMs
                state = state.copy(phase = HomePhase.Listening, listeningSeconds = 0)
            }
        }
    }

    private fun fail(nowMs: Long, reason: HomeFailure) {
        endedAt = nowMs
        state = state.copy(phase = HomePhase.Failed, armProgress = 0f, failure = reason)
    }

    companion object {
        /** "mm:ss" for the listening timer. */
        fun formatElapsed(totalSeconds: Int): String =
            "%02d:%02d".format(java.util.Locale.ROOT, totalSeconds / 60, totalSeconds % 60)

        const val HOLD_MS = 1_500L
        const val MIN_PROCESSING_MS = 1_200L
        const val START_TIMEOUT_MS = 4_000L
        const val CANCELLED_MS = 2_000L
        const val FAILED_MS = 4_000L
    }
}
