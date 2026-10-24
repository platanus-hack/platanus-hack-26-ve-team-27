package com.team27.mama.setup

import org.junit.Assert.assertEquals
import org.junit.Assert.assertNotNull
import org.junit.Assert.assertNull
import org.junit.Assert.assertTrue
import org.junit.Test

class SetupFormTest {

    private val valid = SetupForm(
        ownNumber = "0414 123 4567",
        familyNumbers = listOf("0424 111 2233", "+58 412 555 6677"),
        duressPhrase = "  dile a la tía   Carmen que llego tarde ",
    )

    @Test
    fun validFormNormalizesNumbersAndPhrase() {
        assertEquals(
            ValidSetup(
                ownNumber = "+584141234567",
                familyNumbers = listOf("+584241112233", "+584125556677"),
                duressPhrase = "dile a la tía Carmen que llego tarde",
            ),
            valid.validated(),
        )
        assertTrue(valid.errors().isValid)
    }

    @Test
    fun theDefaultFormHasThePitchPhraseAndOneEmptyNumber() {
        val form = SetupForm()
        assertEquals("dile a la tía Carmen que llego tarde", form.duressPhrase)
        assertEquals(listOf(""), form.familyNumbers)
        assertNull(form.validated())
    }

    @Test
    fun eachWrongFieldGetsItsOwnMessage() {
        val errors = valid.copy(ownNumber = "123", familyNumbers = listOf("", "0414 999"), duressPhrase = "hola").errors()
        assertNotNull(errors.ownNumber)
        assertNotNull(errors.familyNumbers[0])
        assertNotNull(errors.familyNumbers[1])
        assertNotNull(errors.duressPhrase)
    }

    @Test
    fun familyCannotRepeatANumberOrBeYourOwn() {
        val errors = valid.copy(familyNumbers = listOf("+584141234567", "0424 111 2233", "0424-1112233")).errors()
        assertNotNull("own number", errors.familyNumbers[0])
        assertNull(errors.familyNumbers[1])
        assertNotNull("duplicate", errors.familyNumbers[2])
        assertNull(valid.copy(familyNumbers = listOf("0424 111 2233", "0424-1112233")).validated())
    }

    @Test
    fun phraseNeedsThreeWords() {
        assertNotNull(valid.copy(duressPhrase = "tía Carmen").errors().duressPhrase)
        assertNull(valid.copy(duressPhrase = "llego tarde hoy").errors().duressPhrase)
    }
}
