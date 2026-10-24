// Copied from bitchat-android (https://github.com/permissionlesstech/bitchat-android)
// commit dfc67dad41691128bc6e7425bd69f2f409cb5bbf, file app/src/main/java/com/bitchat/android/model/RoutedPacket.kt.
// GPL-3.0: see LICENSE and mesh/NOTICE.md. MAMÁ changes: package renamed to com.team27.mama.mesh.

package com.team27.mama.mesh.model

import com.team27.mama.mesh.protocol.BitchatPacket

/**
 * Represents a routed packet with additional metadata
 * Used for processing and routing packets in the mesh network
 */
data class RoutedPacket(
    val packet: BitchatPacket,
    val peerID: String? = null,           // Who sent it (parsed from packet.senderID)
    val relayAddress: String? = null,     // Address it came from (for avoiding loopback)
    val transferId: String? = null,       // Optional stable transfer ID for progress tracking
    /** Exact fragments admitted during private-media prepare; never rebuild them at commit. */
    val preparedPackets: List<BitchatPacket>? = null,
    // Opaque, process-local ingress identity. Unlike relayAddress, this distinguishes replacement
    // sockets for the same provisional peer and must never be serialized onto the mesh.
    val ingressLinkID: String? = null
)
