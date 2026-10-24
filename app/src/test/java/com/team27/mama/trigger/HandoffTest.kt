package com.team27.mama.trigger

import com.team27.mama.contract.AlertResponse
import com.team27.mama.contract.Branch
import org.junit.Assert.assertEquals
import org.junit.Test

class HandoffTest {
    private val id = "9b2e4f1d-3c5a-4e7b-8d6f-1a2b3c4d5e6f"

    private fun accepted(branch: Branch?) =
        AlertOutcome.Accepted(branch?.let { AlertResponse(id, it) }, attempts = 1)

    /** Runs [handOff] and returns what it picked, which launcher ran and with which id. */
    private fun run(outcome: AlertOutcome): Triple<Handoff, List<String>, List<String>> {
        val rang = mutableListOf<String>()
        val shown = mutableListOf<String>()
        val handoff = handOff(id, outcome, { rang += it }) { shown += it }
        return Triple(handoff, rang, shown)
    }

    @Test
    fun theCallBranchRingsTheInAppCall() {
        assertEquals(Triple(Handoff.IncomingCall(id), listOf(id), emptyList<String>()), run(accepted(Branch.CALL)))
    }

    @Test
    fun theOtherBranchesShowTheConfirmationScreen() {
        for (branch in listOf(Branch.IMMEDIATE_ALERT, Branch.SILENT_TRACKER)) {
            assertEquals(Triple(Handoff.Confirmation(id), emptyList<String>(), listOf(id)), run(accepted(branch)))
        }
    }

    @Test
    fun aStoredIncidentWithAnUnreadableAnswerShowsTheConfirmation() {
        assertEquals(Handoff.Confirmation(id), run(accepted(null)).first)
    }

    @Test
    fun anAlertTheServerNeverTookShowsNothing() {
        val none = Triple(Handoff.None, emptyList<String>(), emptyList<String>())
        assertEquals(none, run(AlertOutcome.GaveUp(-1, attempts = 11)))
        assertEquals(none, run(AlertOutcome.Rejected(400, attempts = 1)))
    }
}
