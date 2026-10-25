package com.team27.mama.profile

import com.team27.mama.setup.KeyValueStore
import com.team27.mama.setup.SetupStore
import org.junit.Assert.assertEquals
import org.junit.Assert.assertFalse
import org.junit.Test

class ProfileTest {

    private class MemoryStore(vararg entries: Pair<String, String>) : KeyValueStore {
        val values = mutableMapOf<String, Any?>(*entries)
        override fun getString(key: String) = values[key] as String?
        override fun putString(key: String, value: String?) {
            values[key] = value
        }
        override fun getBoolean(key: String) = values[key] as Boolean? ?: false
        override fun putBoolean(key: String, value: Boolean) {
            values[key] = value
        }
    }

    @Test
    fun maskKeepsOnlyTheLastFourDigits() {
        assertEquals("•••• 4567", PhoneMask.mask("+584141234567"))
        assertEquals("•••• 4567", PhoneMask.mask("+58 414 123 4567"))
        assertEquals("•••• 6789", PhoneMask.mask("+14155556789"))
    }

    @Test
    fun maskHidesAShortNumberCompletely() {
        assertEquals("••••", PhoneMask.mask("1234"))
        assertEquals("••••", PhoneMask.mask(""))
    }

    @Test
    fun profileShowsWhatSetupSavedWithNumbersMasked() {
        val store = SetupStore(
            MemoryStore(
                "user_name" to "Mariana",
                "own_number" to "+584141234567",
                "family_numbers" to "+584121112233,+584249998877",
                "duress_phrase" to "dile a la tía Carmen que llego tarde",
                "safe_phrase" to "ya llegué a la casa",
            ),
        )

        assertEquals(
            Profile(
                name = "Mariana",
                ownNumber = "•••• 4567",
                familyNumbers = listOf("•••• 2233", "•••• 8877"),
                alertPhrase = "dile a la tía Carmen que llego tarde",
                safePhrase = "ya llegué a la casa",
            ),
            Profile.from(store),
        )
    }

    @Test
    fun profileWithNothingSavedIsEmptyInsteadOfCrashing() {
        assertEquals(
            Profile(name = null, ownNumber = null, familyNumbers = emptyList(), alertPhrase = null, safePhrase = null),
            Profile.from(SetupStore(MemoryStore())),
        )
    }

    @Test
    fun blankNameAndPhrasesCountAsNotSetUp() {
        val store = SetupStore(MemoryStore("user_name" to "  ", "duress_phrase" to "", "safe_phrase" to " "))

        val profile = Profile.from(store)

        assertEquals(null, profile.name)
        assertEquals(null, profile.alertPhrase)
        assertEquals(null, profile.safePhrase)
    }

    @Test
    fun readingTheProfileDoesNotMakeADeviceId() {
        val kv = MemoryStore()

        Profile.from(SetupStore(kv) { "id" })

        assertFalse("device_id" in kv.values)
    }
}
