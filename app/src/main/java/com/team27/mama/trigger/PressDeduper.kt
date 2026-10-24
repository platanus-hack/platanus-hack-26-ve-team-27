package com.team27.mama.trigger

/** Where a volume press came from. */
enum class PressSource { ACCESSIBILITY, MEDIA_SESSION }

/**
 * One physical press can reach both the accessibility service and the media
 * session (screen on, or the always-on display lit). Accepts a press unless
 * the last accepted one was the same key from the other source less than
 * [windowMs] earlier. The window can be wide: up-down-up never repeats a key,
 * and on the Xiaomi the session copy of a press came up to ~390 ms late.
 */
class PressDeduper(private val windowMs: Long = 1_000) {
    private var last: Triple<VolumeKey, PressSource, Long>? = null

    fun accept(key: VolumeKey, source: PressSource, atMs: Long): Boolean {
        val previous = last
        if (previous != null && previous.first == key && previous.second != source && atMs - previous.third < windowMs) return false
        last = Triple(key, source, atMs)
        return true
    }
}
