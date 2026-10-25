package com.team27.mama.trigger

import com.team27.mama.contract.AlertResponse
import com.team27.mama.contract.Branch
import com.team27.mama.contract.Trigger
import org.junit.Assert.assertEquals
import org.junit.Assert.assertFalse
import org.junit.Assert.assertTrue
import org.junit.Test

class TriggerProgressTest {
    private val id = "9b2e4f1d-3c5a-4e7b-8d6f-1a2b3c4d5e6f"

    @Test
    fun anAcceptedAlertIsActive() {
        assertEquals(TriggerProgress.Active, progressAfterAlert(AlertOutcome.Accepted(AlertResponse(id, Branch.CALL), attempts = 1)))
        assertEquals(TriggerProgress.Active, progressAfterAlert(AlertOutcome.Accepted(null, attempts = 2)))
    }

    @Test
    fun anAlertTheServerNeverTookKeepsSendingUntilTheFallbackDecides() {
        assertEquals(TriggerProgress.Sending, progressAfterAlert(AlertOutcome.Rejected(422, attempts = 1)))
        assertEquals(TriggerProgress.Sending, progressAfterAlert(AlertOutcome.GaveUp(0, attempts = 3)))
    }

    @Test
    fun onlyTheInAppButtonSkipsTheConfirmationScreen() {
        assertFalse(opensConfirmation(Trigger.IN_APP))
        assertTrue(Trigger.entries.filter { it != Trigger.IN_APP }.all(::opensConfirmation))
    }
}
