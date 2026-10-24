package com.team27.mama.spike

import com.team27.mama.spike.PressSource.ACCESSIBILITY
import com.team27.mama.spike.PressSource.MEDIA_SESSION
import org.junit.Assert.assertEquals
import org.junit.Assert.assertTrue
import org.junit.Test

class PressDeduperTest {

    @Test
    fun theSamePressFromBothSourcesCountsOnce() {
        val d = PressDeduper(windowMs = 250)
        assertEquals(
            listOf(true, false, true, false),
            listOf(
                d.accept(VolumeKey.UP, ACCESSIBILITY, 0),
                d.accept(VolumeKey.UP, MEDIA_SESSION, 20),
                d.accept(VolumeKey.DOWN, MEDIA_SESSION, 300),
                d.accept(VolumeKey.DOWN, ACCESSIBILITY, 310),
            ),
        )
    }

    @Test
    fun repeatedPressesFromOneSourceAllCount() {
        val d = PressDeduper()
        assertTrue(d.accept(VolumeKey.UP, MEDIA_SESSION, 0))
        assertTrue(d.accept(VolumeKey.UP, MEDIA_SESSION, 100))
    }

    @Test
    fun theOtherSourceCountsOutsideTheWindowOrForAnotherKey() {
        val d = PressDeduper(windowMs = 250)
        d.accept(VolumeKey.UP, ACCESSIBILITY, 0)
        assertTrue(d.accept(VolumeKey.UP, MEDIA_SESSION, 250))
        assertTrue(d.accept(VolumeKey.DOWN, ACCESSIBILITY, 260))
    }

    @Test
    fun aDoubledStreamStillMatchesThePatternOnce() {
        val d = PressDeduper()
        val pattern = VolumePattern()
        val presses = listOf(
            Triple(VolumeKey.UP, ACCESSIBILITY, 0L), Triple(VolumeKey.UP, MEDIA_SESSION, 15L),
            Triple(VolumeKey.DOWN, ACCESSIBILITY, 300L), Triple(VolumeKey.DOWN, MEDIA_SESSION, 310L),
            Triple(VolumeKey.UP, MEDIA_SESSION, 600L), Triple(VolumeKey.UP, ACCESSIBILITY, 612L),
        )
        val matches = presses.filter { (k, s, t) -> d.accept(k, s, t) }.map { (k, _, t) -> pattern.onPress(k, t) }
        assertEquals(listOf(false, false, true), matches)
    }
}
