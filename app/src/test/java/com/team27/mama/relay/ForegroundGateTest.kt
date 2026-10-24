package com.team27.mama.relay

import org.junit.Assert.assertFalse
import org.junit.Assert.assertTrue
import org.junit.Test

class ForegroundGateTest {
    @Test
    fun `stop before the service reached the foreground does not call stopService and cancels the start`() {
        val gate = ForegroundGate()
        gate.onStartRequested()
        assertFalse(gate.onStopRequested())
        assertFalse(gate.onForegrounded())
    }

    @Test
    fun `stop after foreground calls stopService`() {
        val gate = ForegroundGate()
        gate.onStartRequested()
        assertTrue(gate.onForegrounded())
        assertTrue(gate.onStopRequested())
    }

    @Test
    fun `start again after an early stop keeps the service`() {
        val gate = ForegroundGate()
        gate.onStartRequested()
        gate.onStopRequested()
        gate.onStartRequested()
        assertTrue(gate.onForegrounded())
    }

    @Test
    fun `destroy clears the foreground flag`() {
        val gate = ForegroundGate()
        gate.onStartRequested()
        gate.onForegrounded()
        gate.onDestroyed()
        assertFalse(gate.onStopRequested())
        assertFalse(gate.inForeground)
    }
}
