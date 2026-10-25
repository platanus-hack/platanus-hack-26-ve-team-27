package com.team27.mama.trigger

import com.team27.mama.contract.Trigger

/**
 * Where the online trigger is, as far as a screen needs to know. [TriggerService.progress]
 * publishes it; the home screen turns it into "calling" and "listening".
 */
enum class TriggerProgress {
    /** No incident is running (never started, ignored, cancelled or finished). */
    Idle,

    /** An incident started and the alert is not answered yet. */
    Sending,

    /** The server accepted the alert, or the offline emergency took over. */
    Active,
}

/** The progress right after the alert was answered: only an accepted alert is [TriggerProgress.Active]. */
fun progressAfterAlert(outcome: AlertOutcome): TriggerProgress =
    if (outcome is AlertOutcome.Accepted) TriggerProgress.Active else TriggerProgress.Sending

/**
 * Whether the hand-off may open the confirmation screen. The in-app button already shows its
 * progress on the home screen, so it never opens a second screen on top.
 */
fun opensConfirmation(trigger: Trigger): Boolean = trigger != Trigger.IN_APP
