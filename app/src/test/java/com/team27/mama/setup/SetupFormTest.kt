package com.team27.mama.setup

import org.junit.Assert.assertEquals
import org.junit.Assert.assertNotNull
import org.junit.Assert.assertNull
import org.junit.Assert.assertTrue
import org.junit.Test

class SetupFormTest {

    private val valid = SetupForm(
        userName = " Dani ",
        ownNumber = "0414 123 4567",
        familyNumbers = listOf("0424 111 2233", "+58 412 555 6677"),
        duressPhrase = "  dile a la tía   Carmen que llego tarde ",
        safePhrase = " voy llegando,  ya casi ",
    )

    @Test
    fun validFormNormalizesNumbersAndPhrase() {
        assertEquals(
            ValidSetup(
                userName = "Dani",
                ownNumber = "+584141234567",
                familyNumbers = listOf("+584241112233", "+584125556677"),
                duressPhrase = "dile a la tía Carmen que llego tarde",
                safePhrase = "voy llegando, ya casi",
            ),
            valid.validated(),
        )
        assertTrue(valid.errors().isValid)
    }

    @Test
    fun theDefaultFormHasThePitchPhraseAndOneEmptyNumber() {
        val form = SetupForm()
        assertEquals("dile a la tía Carmen que llego tarde", form.duressPhrase)
        assertEquals("voy llegando, ya casi", form.safePhrase)
        assertEquals("", form.userName)
        assertNotNull("the name has no default", form.errors().userName)
        assertNull("the two default phrases may be kept together", form.errors().safePhrase)
        assertEquals(listOf(""), form.familyNumbers)
        assertNull(form.validated())
    }

    @Test
    fun eachWrongFieldGetsItsOwnMessage() {
        val errors = valid.copy(ownNumber = "123", familyNumbers = listOf("", "0414 999"), duressPhrase = " ").errors()
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
    fun ownNumberMustBeVenezuelanButFamilyCanBeAnywhere() {
        assertNotNull(valid.copy(ownNumber = "+1 555 010 0123").errors().ownNumber)
        assertNull(valid.copy(ownNumber = "+1 555 010 0123").validated())
        assertNull(valid.copy(familyNumbers = listOf("+34 612 345 678")).errors().familyNumbers[0])
    }

    @Test
    fun phraseNeedsOneWord() {
        assertNotNull(valid.copy(duressPhrase = "  ").errors().duressPhrase)
        assertNotNull(valid.copy(safePhrase = "¡!").errors().safePhrase)
        assertNull(valid.copy(duressPhrase = "Carmen").errors().duressPhrase)
        assertNull(valid.copy(safePhrase = "todo bien").errors().safePhrase)
    }

    @Test
    fun nameIsOneToThirtyCharacters() {
        assertNotNull(valid.copy(userName = "   ").errors().userName)
        assertNull(valid.copy(userName = "D").errors().userName)
        assertNull(valid.copy(userName = "Á".repeat(30)).errors().userName)
        assertNotNull(valid.copy(userName = "a".repeat(31)).errors().userName)
        assertNull(valid.copy(userName = "").validated())
    }

    @Test
    fun phrasesOverTheServerCapsAreRejected() {
        val words21 = (1..21).joinToString(" ") { "palabra$it" }
        assertNotNull(valid.copy(safePhrase = words21).errors().safePhrase)
        assertNotNull(valid.copy(duressPhrase = "una frase " + "x".repeat(150)).errors().duressPhrase)
    }

    @Test
    fun safePhraseMustDifferFromTheAlertPhrase() {
        val same = valid.copy(safePhrase = "Dile a la TIA carmen, que llegó tarde.")
        assertEquals("Tiene que ser distinta de tu frase de alerta.", same.errors().safePhrase)
        assertNull(same.validated())
    }

    @Test
    fun safePhraseMustNotShareMostOfTheAlertPhrasesKeyWords() {
        // dile, tia, carmen, llego, tarde: the key words of the alert phrase.
        fun safeError(phrase: String) = valid.copy(safePhrase = phrase).errors().safePhrase
        assertEquals(TOO_CLOSE_TO_DURESS, safeError("dile a la tía Carmen que llego temprano"))
        assertEquals(TOO_CLOSE_TO_DURESS, safeError("Carmen, ya llego, no es tarde"))
        assertEquals("a one-letter typo still counts", TOO_CLOSE_TO_DURESS, safeError("dile a la tía Carmem"))
        assertNull("without the typo match it would be two of five", safeError("dile a la tía María"))
        assertNull("two of five key words is fine", safeError("la tía Carmen ya me buscó"))
        assertNull(safeError("voy llegando, ya casi"))
    }
}
