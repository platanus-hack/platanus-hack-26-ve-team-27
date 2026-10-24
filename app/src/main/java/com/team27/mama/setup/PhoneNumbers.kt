package com.team27.mama.setup

/**
 * Turns what the person types into E.164 (docs/contract.md: phone numbers are
 * `+58...`). Venezuelan numbers can be typed the local way: `0414 123 4567`,
 * `414-1234567` or `+58 414 123 4567` all give `+584141234567`. Foreign
 * numbers need the `+` (or `00`) and the country code.
 */
object PhoneNumbers {
    private const val VENEZUELA = "58"

    /** What every Venezuelan number starts with in E.164. */
    const val VENEZUELA_PREFIX = "+$VENEZUELA"

    /** A Venezuelan national number (area or mobile code + subscriber) has 10 digits. */
    private const val VENEZUELA_NATIONAL_DIGITS = 10

    private val e164 = Regex("""^\+[1-9]\d{7,14}$""")
    private val allowedSeparators = Regex("""[\s\-().]""")

    /** The E.164 form of [input], or null when it is not a phone number we can call. */
    fun normalize(input: String): String? {
        val compact = input.trim().replace(allowedSeparators, "")
        if (compact.isEmpty()) return null
        val candidate = when {
            compact.startsWith("+") -> compact
            compact.startsWith("00") -> "+" + compact.removePrefix("00")
            compact.startsWith("0") -> "+$VENEZUELA" + compact.removePrefix("0")
            compact.length == VENEZUELA_NATIONAL_DIGITS -> "+$VENEZUELA$compact"
            else -> return null
        }
        if (!e164.matches(candidate)) return null
        // A +58 number with the wrong length is almost always a typo.
        if (candidate.startsWith("+$VENEZUELA") &&
            candidate.length != 1 + VENEZUELA.length + VENEZUELA_NATIONAL_DIGITS
        ) {
            return null
        }
        return candidate
    }

    /** [e164] grouped for reading, `+58 414 123 4567`; other countries stay as they are. */
    fun display(e164: String): String {
        val national = e164.removePrefix("+$VENEZUELA")
        if (national == e164 || national.length != VENEZUELA_NATIONAL_DIGITS) return e164
        return "+$VENEZUELA ${national.take(3)} ${national.substring(3, 6)} ${national.substring(6)}"
    }

    /** True when [a] and [b] are the same number, ignoring formatting. */
    fun sameNumber(a: String, b: String): Boolean {
        val left = normalize(a) ?: a.filter(Char::isDigit)
        val right = normalize(b) ?: b.filter(Char::isDigit)
        return left.isNotEmpty() && left.filter(Char::isDigit) == right.filter(Char::isDigit)
    }
}
