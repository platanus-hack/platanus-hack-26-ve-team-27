package com.team27.mama.relay

import org.junit.Assert.assertEquals
import org.junit.Test

class RelayControlsTest {
    @Test
    fun counterText() {
        assertEquals("Reenviaste 0 alertas", relayedCountText(0))
        assertEquals("Reenviaste 1 alerta", relayedCountText(1))
        assertEquals("Reenviaste 2 alertas", relayedCountText(2))
        assertEquals("Reenviaste 12 alertas", relayedCountText(12))
    }
}
