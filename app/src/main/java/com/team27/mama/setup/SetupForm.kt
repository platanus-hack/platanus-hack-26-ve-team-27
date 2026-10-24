package com.team27.mama.setup

/** What the person typed in the setup form, as typed. */
data class SetupForm(
    val ownNumber: String = "",
    val familyNumbers: List<String> = listOf(""),
    val duressPhrase: String = DEFAULT_DURESS_PHRASE,
) {
    companion object {
        /** The phrase from the pitch; the person can change it. */
        const val DEFAULT_DURESS_PHRASE = "dile a la tía Carmen que llego tarde"
        const val MAX_FAMILY_NUMBERS = 5

        /** Fewer words than this is too easy to say by accident. */
        const val MIN_PHRASE_WORDS = 3
    }
}

/** The form after validation: numbers in E.164, phrase trimmed. */
data class ValidSetup(val ownNumber: String, val familyNumbers: List<String>, val duressPhrase: String)

/** One Spanish message per field that is wrong; all null when the form is valid. */
data class SetupFormErrors(
    val ownNumber: String? = null,
    val familyNumbers: List<String?> = emptyList(),
    val duressPhrase: String? = null,
) {
    val isValid: Boolean get() = ownNumber == null && duressPhrase == null && familyNumbers.all { it == null }
}

private const val INVALID_NUMBER = "Revisa el número. Ejemplo: 0414 123 4567 o +58 414 123 4567."

fun SetupForm.errors(): SetupFormErrors {
    val own = PhoneNumbers.normalize(ownNumber)
    val normalized = familyNumbers.map(PhoneNumbers::normalize)
    val familyErrors = familyNumbers.mapIndexed { index, typed ->
        val number = normalized[index]
        when {
            typed.isBlank() -> "Escribe el número de alguien de tu familia."
            number == null -> INVALID_NUMBER
            number == own -> "Este es tu número. Escribe el de alguien de tu familia."
            normalized.indexOf(number) < index -> "Ya agregaste este número."
            else -> null
        }
    }
    val phraseWords = duressPhrase.trim().split(Regex("""\s+""")).filter(String::isNotEmpty)
    return SetupFormErrors(
        ownNumber = when {
            ownNumber.isBlank() -> "Escribe tu número para que Mamá te pueda llamar."
            own == null -> INVALID_NUMBER
            else -> null
        },
        familyNumbers = familyErrors,
        duressPhrase = if (phraseWords.size < SetupForm.MIN_PHRASE_WORDS) {
            "Usa una frase de al menos ${SetupForm.MIN_PHRASE_WORDS} palabras que no dirías por casualidad."
        } else {
            null
        },
    )
}

/** The validated values, or null when [errors] has any message. */
fun SetupForm.validated(): ValidSetup? {
    if (!errors().isValid) return null
    return ValidSetup(
        ownNumber = PhoneNumbers.normalize(ownNumber)!!,
        familyNumbers = familyNumbers.map { PhoneNumbers.normalize(it)!! },
        duressPhrase = duressPhrase.trim().replace(Regex("""\s+"""), " "),
    )
}
