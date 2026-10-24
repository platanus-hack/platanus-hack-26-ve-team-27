package com.team27.mama.mesh

import org.junit.Assert.assertThrows
import org.junit.Test

/** A device without BLE is rejected before any component (and its periodic loops) is built. */
class BleMeshNodeTest {

    @Test
    fun missingBleFeature_isRejected() {
        assertThrows(IllegalStateException::class.java) {
            BleMeshNode.requireBleHardware(hasBleFeature = false, bluetoothService = null)
        }
    }

    @Test
    fun missingBluetoothService_isRejected() {
        assertThrows(IllegalStateException::class.java) {
            BleMeshNode.requireBleHardware(hasBleFeature = true, bluetoothService = null)
        }
        assertThrows(IllegalStateException::class.java) {
            BleMeshNode.requireBleHardware(hasBleFeature = true, bluetoothService = Any())
        }
    }
}
