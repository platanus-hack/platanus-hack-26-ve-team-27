package com.team27.mama.setup

import org.junit.Assert.assertEquals
import org.junit.Assert.assertFalse
import org.junit.Assert.assertNull
import org.junit.Assert.assertTrue
import org.junit.Test

class PhoneNumbersTest {

    @Test
    fun venezuelanNumbersTypedTheLocalWayBecomeE164() {
        listOf(
            "04141234567",
            "0414 123 4567",
            "0414-123.45.67",
            "(0414) 1234567",
            "4141234567",
            "+58 414 123 4567",
            "+584141234567",
            "00584141234567",
            "  0414 123 4567  ",
        ).forEach { assertEquals(it, "+584141234567", PhoneNumbers.normalize(it)) }
    }

    @Test
    fun foreignNumbersNeedTheCountryCode() {
        assertEquals("+15550100123", PhoneNumbers.normalize("+1 (555) 010-0123"))
        assertEquals("+34612345678", PhoneNumbers.normalize("0034 612 345 678"))
        // Without + or 00, nine digits is neither Venezuelan nor E.164.
        assertNull(PhoneNumbers.normalize("612345678"))
    }

    @Test
    fun rejectsWhatIsNotACallableNumber() {
        listOf("", "   ", "abc", "0414 123 456", "+58 414 123 45678", "+0123456789", "+12", "0414 123 456a")
            .forEach { assertNull(it, PhoneNumbers.normalize(it)) }
    }

    @Test
    fun displayGroupsVenezuelanNumbersOnly() {
        assertEquals("+58 414 123 4567", PhoneNumbers.display("+584141234567"))
        assertEquals("+15550100123", PhoneNumbers.display("+15550100123"))
    }

    @Test
    fun sameNumberIgnoresFormatting() {
        assertTrue(PhoneNumbers.sameNumber("+584141234567", "0414-123 4567"))
        assertTrue(PhoneNumbers.sameNumber("+1 555 010 0123", "+15550100123"))
        assertFalse(PhoneNumbers.sameNumber("+584141234567", "+584141234568"))
        assertFalse(PhoneNumbers.sameNumber("", ""))
    }
}
