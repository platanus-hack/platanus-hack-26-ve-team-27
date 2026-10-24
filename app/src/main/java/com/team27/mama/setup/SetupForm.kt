package com.team27.mama.setup

/** What the person typed in the setup form, as typed. */
data class SetupForm(
    val userName: String = "",
    val ownNumber: String = "",
    val familyNumbers: List<String> = listOf(""),
    val duressPhrase: String = DEFAULT_DURESS_PHRASE,
    val safePhrase: String = DEFAULT_SAFE_PHRASE,
) {
    companion object {
        /** The phrase from the pitch; the person can change it. */
        const val DEFAULT_DURESS_PHRASE = "dile a la tía Carmen que llego tarde"

        /** The "todo bien" example from #108; it also sounds like an answer to "¿cómo vas?". */
        const val DEFAULT_SAFE_PHRASE = "voy llegando, ya casi"
        const val MAX_FAMILY_NUMBERS = 5

        /** Fewer words than this is too easy to say by accident. */
        const val MIN_PHRASE_WORDS = 3

        /** The server's caps (docs/contract.md, DeviceRegistration): longer gets a 422. */
        const val MAX_NAME_CHARS = 30
        const val MAX_PHRASE_CHARS = 150
        const val MAX_PHRASE_WORDS = 20
    }
}

/** The form after validation: numbers in E.164, name and phrases trimmed. */
data class ValidSetup(
    val userName: String,
    val ownNumber: String,
    val familyNumbers: List<String>,
    val duressPhrase: String,
    val safePhrase: String,
)

/** One Spanish message per field that is wrong; all null when the form is valid. */
data class SetupFormErrors(
    val userName: String? = null,
    val ownNumber: String? = null,
    val familyNumbers: List<String?> = emptyList(),
    val duressPhrase: String? = null,
    val safePhrase: String? = null,
) {
    val isValid: Boolean
        get() = userName == null && ownNumber == null && duressPhrase == null && safePhrase == null &&
            familyNumbers.all { it == null }
}

/** The safe phrase shares most of the duress phrase's key words; the server's 422 shows it too. */
internal const val TOO_CLOSE_TO_DURESS = "Se parece mucho a tu frase de alerta. Usa otras palabras."

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
    val name = userName.trim()
    return SetupFormErrors(
        userName = when {
            name.isEmpty() -> "Escribe cómo te llama Mamá."
            name.codePointCount(0, name.length) > SetupForm.MAX_NAME_CHARS ->
                "Usa ${SetupForm.MAX_NAME_CHARS} letras o menos."
            else -> null
        },
        ownNumber = when {
            ownNumber.isBlank() -> "Escribe tu número para que Mamá te pueda llamar."
            own == null -> INVALID_NUMBER
            // The server only dials +58 (server/internal/call/engine.go), so any other number would never ring.
            !own.startsWith(PhoneNumbers.VENEZUELA_PREFIX) -> "Por ahora Mamá solo llama a números de Venezuela (+58)."
            else -> null
        },
        familyNumbers = familyErrors,
        duressPhrase = phraseError(duressPhrase),
        safePhrase = phraseError(safePhrase) ?: when {
            PhraseSimilarity.words(safePhrase) == PhraseSimilarity.words(duressPhrase) ->
                "Tiene que ser distinta de tu frase de alerta."
            PhraseSimilarity.tooClose(safePhrase, duressPhrase) ->
                TOO_CLOSE_TO_DURESS
            else -> null
        },
    )
}

/** Too short to be safe from saying it by accident, or over the server's caps. */
private fun phraseError(phrase: String): String? {
    val words = PhraseSimilarity.words(phrase).size
    return when {
        words < SetupForm.MIN_PHRASE_WORDS ->
            "Usa una frase de al menos ${SetupForm.MIN_PHRASE_WORDS} palabras que no dirías por casualidad."
        words > SetupForm.MAX_PHRASE_WORDS || phrase.trim().let { it.codePointCount(0, it.length) } > SetupForm.MAX_PHRASE_CHARS ->
            "Usa una frase más corta: de ${SetupForm.MAX_PHRASE_WORDS} palabras y ${SetupForm.MAX_PHRASE_CHARS} letras o menos."
        else -> null
    }
}

private fun normalizedPhrase(phrase: String) = phrase.trim().replace(Regex("""\s+"""), " ")

/** The validated values, or null when [errors] has any message. */
fun SetupForm.validated(): ValidSetup? {
    if (!errors().isValid) return null
    return ValidSetup(
        userName = userName.trim(),
        ownNumber = PhoneNumbers.normalize(ownNumber)!!,
        familyNumbers = familyNumbers.map { PhoneNumbers.normalize(it)!! },
        duressPhrase = normalizedPhrase(duressPhrase),
        safePhrase = normalizedPhrase(safePhrase),
    )
}
