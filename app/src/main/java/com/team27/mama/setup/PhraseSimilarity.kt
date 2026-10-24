package com.team27.mama.setup

import java.text.Normalizer

/**
 * The phone-side copy of the server's "too close" rule for the safe phrase
 * (`tooCloseToDuress` in server/internal/intake, built on server/internal/duress):
 * the same normalization (case, accents, stopwords, one-letter typos) and the
 * same "half or more of the key words" share. It checks the whole safe phrase
 * at once, so it may say "too close" where the server's sliding windows would
 * not; the server stays the judge and its 422 is shown on the field too.
 */
object PhraseSimilarity {
    private const val PARTIAL_SHARE = 0.5

    // Kept equal to the stopwords in server/internal/duress/duress.go.
    private val STOPWORDS = setOf(
        "a", "al", "con", "de", "del", "el", "en", "la", "las", "le", "les", "lo", "los",
        "me", "mi", "mis", "o", "para", "por", "que", "se", "su", "te", "tu", "un", "una", "unas", "unos", "y", "ya", "yo",
    )

    private val NON_WORD = Regex("""[^\p{L}\p{N}]+""")
    private val MARKS = Regex("""\p{M}+""")

    /** Runs of letters and digits, lowercased, without accents ("Tía" -> "tia", "ñ" -> "n"). */
    fun words(phrase: String): List<String> =
        Normalizer.normalize(phrase.lowercase(), Normalizer.Form.NFD)
            .replace(MARKS, "")
            .split(NON_WORD)
            .filter(String::isNotEmpty)

    /** True when the duress detector would likely hear [safe], said alone, as [duress]. */
    fun tooClose(safe: String, duress: String): Boolean {
        val duressWords = words(duress)
        val safeWords = words(safe)
        if (duressWords.isEmpty() || safeWords.isEmpty()) return false
        if (duressWords == safeWords) return true
        val keys = duressWords.filterNot(STOPWORDS::contains).distinct().ifEmpty { duressWords.distinct() }
        val matched = keys.count { key -> safeWords.any { wordMatches(key, it) } }
        return matched == keys.size || (matched >= 2 && matched >= keys.size * PARTIAL_SHARE)
    }

    private fun wordMatches(key: String, word: String): Boolean =
        key == word || (key.codePointCount(0, key.length) >= 5 && editDistanceAtMostOne(key, word))

    private fun editDistanceAtMostOne(a: String, b: String): Boolean {
        var long = a.codePoints().toArray()
        var short = b.codePoints().toArray()
        if (long.size < short.size) long = short.also { short = long }
        if (long.size - short.size > 1) return false
        var i = 0
        var j = 0
        var edits = 0
        while (i < long.size && j < short.size) {
            if (long[i] == short[j]) {
                i++
                j++
                continue
            }
            if (++edits > 1) return false
            if (long.size == short.size) j++
            i++
        }
        return edits + (long.size - i) <= 1
    }
}
