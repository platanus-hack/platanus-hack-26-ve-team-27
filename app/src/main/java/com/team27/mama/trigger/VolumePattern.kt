package com.team27.mama.trigger

enum class VolumeKey { UP, DOWN }

/**
 * Detects up-down-up on the volume keys. Feed it each key press (key down,
 * not repeats) with a monotonic time in ms; it returns true once per pattern.
 * Each gap between presses must be at most [maxGapMs]; after a match it stays
 * quiet for [cooldownMs] so one gesture never fires twice. 1.5 s per gap:
 * blind presses on the Xiaomi took up to 1.28 s.
 */
class VolumePattern(private val maxGapMs: Long = 1_500, private val cooldownMs: Long = 5_000) {
    private val presses = ArrayDeque<Pair<VolumeKey, Long>>()
    private var lastMatchAt: Long? = null

    fun onPress(key: VolumeKey, atMs: Long): Boolean {
        presses.addLast(key to atMs)
        while (presses.size > PATTERN.size) presses.removeFirst()
        if (presses.size < PATTERN.size) return false
        if (presses.map { it.first } != PATTERN) return false
        if (presses.zipWithNext().any { (a, b) -> b.second - a.second > maxGapMs }) return false
        val last = lastMatchAt
        if (last != null && atMs - last < cooldownMs) return false
        lastMatchAt = atMs
        presses.clear()
        return true
    }

    private companion object {
        val PATTERN = listOf(VolumeKey.UP, VolumeKey.DOWN, VolumeKey.UP)
    }
}
