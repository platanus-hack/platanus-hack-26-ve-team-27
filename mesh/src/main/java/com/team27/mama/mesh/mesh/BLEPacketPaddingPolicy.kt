// Copied from bitchat-android (https://github.com/permissionlesstech/bitchat-android)
// commit dfc67dad41691128bc6e7425bd69f2f409cb5bbf, file app/src/main/java/com/bitchat/android/mesh/BLEPacketPaddingPolicy.kt.
// GPL-3.0: see LICENSE and mesh/NOTICE.md. MAMÁ changes: package renamed to com.team27.mama.mesh.

package com.team27.mama.mesh.mesh

import com.team27.mama.mesh.protocol.MessageType

/**
 * iOS-compatible BLE padding policy.
 *
 * Keep this aligned with iOS BLEOutboundPacketPolicy.padsBLEFrame(for:):
 * only Noise frames are padded over BLE.
 */
object BLEPacketPaddingPolicy {
    fun shouldPadForBLE(type: UByte): Boolean {
        return when (MessageType.fromValue(type)) {
            MessageType.NOISE_ENCRYPTED, MessageType.NOISE_HANDSHAKE -> true
            else -> false
        }
    }
}
