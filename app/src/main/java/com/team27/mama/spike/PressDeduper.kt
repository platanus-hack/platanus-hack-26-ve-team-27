package com.team27.mama.spike

/** Where a volume press came from. */
enum class PressSource { ACCESSIBILITY, MEDIA_SESSION }

/**
 * With the screen on, one physical press can reach both the accessibility
 * service and the media session. Accepts a press unless the last accepted one
 * was the same key from the other source less than [windowMs] earlier.
 */
class PressDeduper(private val windowMs: Long = 250) {
    private var last: Triple<VolumeKey, PressSource, Long>? = null

    fun accept(key: VolumeKey, source: PressSource, atMs: Long): Boolean {
        val previous = last
        if (previous != null && previous.first == key && previous.second != source && atMs - previous.third < windowMs) return false
        last = Triple(key, source, atMs)
        return true
    }
}
