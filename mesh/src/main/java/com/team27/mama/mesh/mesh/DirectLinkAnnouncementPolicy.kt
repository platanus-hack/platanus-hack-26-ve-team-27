// Copied from bitchat-android (https://github.com/permissionlesstech/bitchat-android)
// commit dfc67dad41691128bc6e7425bd69f2f409cb5bbf, file app/src/main/java/com/bitchat/android/mesh/DirectLinkAnnouncementPolicy.kt.
// GPL-3.0: see LICENSE and mesh/NOTICE.md. MAMÁ changes: package renamed to com.team27.mama.mesh.

package com.team27.mama.mesh.mesh

import com.team27.mama.mesh.model.RoutedPacket

/**
 * Describes transport reachability learned from an already-validated ANNOUNCE.
 *
 * This is deliberately only a routing observation. Noise authenticates the peer independently and
 * must not be restarted merely to associate the current transport link with that peer.
 */
internal object DirectLinkAnnouncementPolicy {
    data class Observation(
        val peerID: String,
        val relayAddress: String,
        val ingressLinkID: String
    )

    fun observationFor(routed: RoutedPacket, maxTtl: UByte): Observation? {
        if (routed.packet.ttl != maxTtl) return null
        return Observation(
            peerID = routed.peerID ?: return null,
            relayAddress = routed.relayAddress ?: return null,
            ingressLinkID = routed.ingressLinkID ?: return null
        )
    }
}
