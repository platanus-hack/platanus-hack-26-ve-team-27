package com.team27.mama.trigger

import com.team27.mama.trigger.VolumeKey.DOWN
import com.team27.mama.trigger.VolumeKey.UP
import org.junit.Assert.assertEquals
import org.junit.Assert.assertFalse
import org.junit.Assert.assertTrue
import org.junit.Test

class VolumePatternTest {

    private fun VolumePattern.feed(vararg presses: Pair<VolumeKey, Long>): List<Boolean> =
        presses.map { (key, at) -> onPress(key, at) }

    @Test
    fun upDownUpMatchesOnTheLastPress() {
        assertEquals(listOf(false, false, true), VolumePattern().feed(UP to 0, DOWN to 300, UP to 600))
    }

    @Test
    fun otherOrdersDoNotMatch() {
        val pattern = VolumePattern()
        assertFalse(pattern.feed(DOWN to 0, UP to 200, DOWN to 400).any { it })
        assertFalse(VolumePattern().feed(UP to 0, UP to 200, UP to 400).any { it })
        // Down-down (the camera shortcut) never matches.
        assertFalse(VolumePattern().feed(DOWN to 0, DOWN to 200).any { it })
    }

    @Test
    fun aGapLongerThanTheLimitBreaksThePattern() {
        assertFalse(VolumePattern(maxGapMs = 1_000).feed(UP to 0, DOWN to 1_001, UP to 1_500).any { it })
        assertTrue(VolumePattern(maxGapMs = 1_000).feed(UP to 0, DOWN to 1_000, UP to 2_000).last())
    }

    @Test
    fun theWindowSlidesOverEarlierPresses() {
        // A stray down before the gesture does not spoil it.
        assertEquals(listOf(false, false, false, true), VolumePattern().feed(DOWN to 0, UP to 100, DOWN to 200, UP to 300))
        // An old up followed by a fresh up-down-up still matches.
        assertTrue(VolumePattern().feed(UP to 0, UP to 5_000, DOWN to 5_200, UP to 5_400).last())
    }

    @Test
    fun oneGestureFiresOnceAndTheCooldownHolds() {
        val pattern = VolumePattern(cooldownMs = 5_000)
        assertTrue(pattern.feed(UP to 0, DOWN to 200, UP to 400).last())
        // The trailing down-up after a match does not reuse the last up.
        assertFalse(pattern.feed(DOWN to 600, UP to 800).any { it })
        assertFalse(pattern.feed(UP to 1_000, DOWN to 1_200, UP to 1_400).any { it })
        assertTrue(pattern.feed(UP to 6_000, DOWN to 6_200, UP to 6_400).last())
    }

    @Test
    fun theDefaultGapAllowsBlindPresses() {
        // Seen on the Xiaomi: 0.96 s and 1.28 s between blind presses.
        assertTrue(VolumePattern().feed(UP to 0, DOWN to 960, UP to 2_240).last())
        assertFalse(VolumePattern().feed(UP to 0, DOWN to 1_501, UP to 2_000).any { it })
    }
}
