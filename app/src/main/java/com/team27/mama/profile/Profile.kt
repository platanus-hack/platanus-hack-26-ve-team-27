package com.team27.mama.profile

import com.team27.mama.setup.SetupStore

/**
 * What the read-only profile (#85) shows, built from what setup saved. Numbers
 * arrive already masked; a null field was never set up (the name and the
 * "todo bien" phrase stay null until #108 asks for them).
 */
data class Profile(
    val name: String?,
    val ownNumber: String?,
    val familyNumbers: List<String>,
    val alertPhrase: String?,
    val safePhrase: String?,
) {
    companion object {
        fun from(store: SetupStore): Profile = Profile(
            name = store.userName.orNullIfBlank(),
            ownNumber = store.ownNumber.orNullIfBlank()?.let(PhoneMask::mask),
            familyNumbers = store.familyNumbers.map(PhoneMask::mask),
            alertPhrase = store.duressPhrase.orNullIfBlank(),
            safePhrase = store.safePhrase.orNullIfBlank(),
        )
    }
}

private fun String?.orNullIfBlank(): String? = this?.trim()?.takeIf(String::isNotEmpty)

/** Hides a phone number on screen: only its last digits stay, `+584141234567` gives `•••• 4567`. */
object PhoneMask {
    private const val VISIBLE_DIGITS = 4
    private const val DOTS = "••••"

    fun mask(number: String): String {
        val digits = number.filter(Char::isDigit)
        // A number this short would be shown whole; hide it all instead.
        if (digits.length <= VISIBLE_DIGITS) return DOTS
        return "$DOTS ${digits.takeLast(VISIBLE_DIGITS)}"
    }
}
