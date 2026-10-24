package com.team27.mama.relay

import org.junit.Assert.assertEquals
import org.junit.Assert.assertFalse
import org.junit.Assert.assertTrue
import org.junit.Test

class RelayControlsTest {
    @Test
    fun counterText() {
        assertEquals("Reenviaste 0 alertas", relayedCountText(0))
        assertEquals("Reenviaste 1 alerta", relayedCountText(1))
        assertEquals("Reenviaste 2 alertas", relayedCountText(2))
        assertEquals("Reenviaste 12 alertas", relayedCountText(12))
    }

    @Test
    fun permissionCardShowsForARefusedToggleOrARevokedPermission() {
        // A permission revoked in system settings while the relay is on.
        assertTrue(relayPermissionMissing(enabled = true, satisfied = false, refused = false))
        // The user just refused the prompt (the switch stays off).
        assertTrue(relayPermissionMissing(enabled = false, satisfied = false, refused = true))
        assertFalse(relayPermissionMissing(enabled = false, satisfied = false, refused = false))
        assertFalse(relayPermissionMissing(enabled = true, satisfied = true, refused = false))
        assertFalse(relayPermissionMissing(enabled = false, satisfied = true, refused = false))
    }
}
