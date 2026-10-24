package com.team27.mama.setup

import org.junit.Assert.assertEquals
import org.junit.Test

class MamaContactPlanTest {
    private val twilio = "+15550100123"

    @Test
    fun firstRunInsertsMama() {
        assertEquals(ContactPlan.Insert, planMamaContact(twilio, ours = null, namedMama = emptyList()))
    }

    @Test
    fun rerunKeepsOurContactInsteadOfAddingAnother() {
        val ours = RawContactInfo(7, MAMA_CONTACT_NAME, listOf("+1 555-010-0123"))
        assertEquals(ContactPlan.Keep(7), planMamaContact(twilio, ours, namedMama = listOf(ours)))
    }

    @Test
    fun ourContactIsRewrittenWhenEditedOrTheNumberChanged() {
        assertEquals(
            ContactPlan.Rewrite(7),
            planMamaContact(twilio, RawContactInfo(7, MAMA_CONTACT_NAME, listOf("+15550100999")), emptyList()),
        )
        assertEquals(ContactPlan.Rewrite(7), planMamaContact(twilio, RawContactInfo(7, "Mami", listOf(twilio)), emptyList()))
        assertEquals(
            ContactPlan.Rewrite(7),
            planMamaContact(twilio, RawContactInfo(7, MAMA_CONTACT_NAME, listOf(twilio, "+584141234567")), emptyList()),
        )
    }

    @Test
    fun aReinstallAdoptsTheMamaThatAlreadyHasTheTwilioNumber() {
        val existing = RawContactInfo(9, MAMA_CONTACT_NAME, listOf("15550100123"))
        assertEquals(ContactPlan.Adopt(9), planMamaContact(twilio, ours = null, namedMama = listOf(existing)))
    }

    @Test
    fun theRealMotherIsNeverTouched() {
        val realMom = RawContactInfo(3, MAMA_CONTACT_NAME, listOf("0414 123 4567"))
        assertEquals(ContactPlan.Insert, planMamaContact(twilio, ours = null, namedMama = listOf(realMom)))
    }
}
