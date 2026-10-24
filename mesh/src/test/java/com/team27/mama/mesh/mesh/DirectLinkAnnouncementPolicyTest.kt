// Copied from bitchat-android (https://github.com/permissionlesstech/bitchat-android)
// commit dfc67dad41691128bc6e7425bd69f2f409cb5bbf, file app/src/test/kotlin/com/bitchat/android/mesh/DirectLinkAnnouncementPolicyTest.kt.
// GPL-3.0: see LICENSE and mesh/NOTICE.md. MAMÁ changes: package renamed to com.team27.mama.mesh.

package com.team27.mama.mesh.mesh

import com.team27.mama.mesh.model.RoutedPacket
import com.team27.mama.mesh.protocol.BitchatPacket
import com.team27.mama.mesh.protocol.MessageType
import org.junit.Assert.assertEquals
import org.junit.Assert.assertNull
import org.junit.Test

class DirectLinkAnnouncementPolicyTest {
    @Test
    fun `accepted max ttl announce is a direct routing observation`() {
        val routed = announce(ttl = MAX_TTL)

        assertEquals(
            DirectLinkAnnouncementPolicy.Observation(PEER_ID, RELAY_ADDRESS, LINK_ID),
            DirectLinkAnnouncementPolicy.observationFor(routed, MAX_TTL)
        )
    }

    @Test
    fun `relayed announce is not a direct routing observation`() {
        assertNull(
            DirectLinkAnnouncementPolicy.observationFor(
                announce(ttl = (MAX_TTL - 1u).toUByte()),
                MAX_TTL
            )
        )
    }

    @Test
    fun `repeated announce remains the same observation without transport authentication state`() {
        val routed = announce(ttl = MAX_TTL)

        val first = DirectLinkAnnouncementPolicy.observationFor(routed, MAX_TTL)
        val second = DirectLinkAnnouncementPolicy.observationFor(routed, MAX_TTL)

        assertEquals(first, second)
    }

    private fun announce(ttl: UByte) = RoutedPacket(
        packet = BitchatPacket(
            version = 1u,
            type = MessageType.ANNOUNCE.value,
            senderID = PEER_ID.hexToBytes(),
            timestamp = 1u,
            payload = byteArrayOf(1),
            ttl = ttl
        ),
        peerID = PEER_ID,
        relayAddress = RELAY_ADDRESS,
        ingressLinkID = LINK_ID
    )

    private fun String.hexToBytes(): ByteArray =
        chunked(2).map { it.toInt(16).toByte() }.toByteArray()

    private companion object {
        const val PEER_ID = "0011223344556677"
        const val RELAY_ADDRESS = "transport-neighbor"
        const val LINK_ID = "current-link"
        val MAX_TTL: UByte = 7u
    }
}
