package com.team27.mama.setup

import org.junit.Assert.assertEquals
import org.junit.Test

class MamaContactPhotoTest {
    @Test
    fun noPhotoRowInsertsOne() {
        assertEquals(PhotoRowPlan.Insert, planPhotoRow(emptyList()))
    }

    @Test
    fun existingPhotoRowIsUpdatedNeverDuplicated() {
        assertEquals(PhotoRowPlan.Update(7), planPhotoRow(listOf(7, 9)))
    }

    @Test
    fun smallImageIsNotUpscaled() {
        assertEquals(200 to 100, fitWithin(200, 100, 720))
    }

    @Test
    fun largeImageKeepsItsAspectOnTheLongestSide() {
        assertEquals(720 to 360, fitWithin(1440, 720, 720))
        assertEquals(180 to 720, fitWithin(450, 1800, 720))
    }

    @Test
    fun extremeAspectNeverCollapsesToZero() {
        assertEquals(720 to 1, fitWithin(100000, 10, 720))
    }
}
