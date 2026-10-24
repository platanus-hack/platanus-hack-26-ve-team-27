package com.team27.mama.mesh

import com.team27.mama.mesh.crypto.EncryptionService
import com.team27.mama.mesh.mesh.AnnouncementIdentityValidator
import com.team27.mama.mesh.model.IdentityAnnouncement
import com.team27.mama.mesh.model.PeerCapabilities
import com.team27.mama.mesh.protocol.BitchatPacket
import com.team27.mama.mesh.protocol.MessageType
import com.team27.mama.mesh.protocol.SpecialRecipients
import com.team27.mama.mesh.util.toHexString
import org.junit.Assert.assertArrayEquals
import org.junit.Assert.assertEquals
import org.junit.Assert.assertNotNull
import org.junit.Assert.assertNull
import org.junit.Assert.assertTrue
import org.junit.Test

/** Our outgoing packets have the exact shape bitchat peers accept and relay. */
class MeshPacketFactoryTest {

    private val crypto = EncryptionService.generatePrivateKeys().let { (x, ed) -> EncryptionService(x, ed) }
    private val factory = MeshPacketFactory(crypto)
    private val now = 1_800_000_000_000L

    @Test
    fun broadcastMessage_hasBitchatPublicShape() {
        val p = factory.broadcastMessage("MAMA1:abc", now)
        assertEquals(MessageType.MESSAGE.value, p.type)
        assertEquals(7, p.ttl.toInt())
        assertArrayEquals(SpecialRecipients.BROADCAST, p.recipientID)
        assertEquals(now.toULong(), p.timestamp)
        assertEquals("MAMA1:abc", String(p.payload, Charsets.UTF_8))
        assertEquals(64, p.signature!!.size)
        assertEquals(factory.myPeerID, p.senderID.toHexString())
        assertTrue(verify(p))
    }

    @Test
    fun announce_passesUpstreamValidator() {
        val a = factory.announce(now)
        val ann = AnnouncementIdentityValidator.verify(a, factory.myPeerID, nowMs = now) { s, d, k ->
            crypto.verifyEd25519Signature(s, d, k)
        }
        assertNotNull(ann)
        assertEquals(PeerCapabilities.NONE, ann!!.capabilities)
        assertEquals(factory.myPeerID, ann.nickname)
        assertEquals(7, a.ttl.toInt())
        assertArrayEquals(crypto.getSigningPublicKey(), ann.signingPublicKey)
    }

    @Test
    fun announceOutsideClockSkew_isRejectedByUpstreamValidator() {
        val a = factory.announce(now)
        val ann = AnnouncementIdentityValidator.verify(a, factory.myPeerID, nowMs = now + 11 * 60_000L) { s, d, k ->
            crypto.verifyEd25519Signature(s, d, k)
        }
        assertNull(ann)
    }

    @Test
    fun messageSurvivesWireRoundTrip_andVerifiesWithAnnouncedKey() {
        val wire = factory.broadcastMessage("MAMA1:" + "x".repeat(300), now).toBinaryData()!!
        val decoded = BitchatPacket.fromBinaryData(wire)!!
        val ann = IdentityAnnouncement.decode(factory.announce(now).payload)!!
        assertTrue(
            crypto.verifyEd25519Signature(decoded.signature!!, decoded.toBinaryDataForSigning()!!, ann.signingPublicKey)
        )
    }

    @Test
    fun broadcastSequence_isAnnounceThenMessage() {
        val seq = factory.broadcastSequence("MAMA1:abc", now)
        assertEquals(listOf(MessageType.ANNOUNCE.value, MessageType.MESSAGE.value), seq.map { it.type })
        seq.forEach { p ->
            assertEquals(64, p.signature!!.size)
            assertTrue(verify(p))
            assertEquals(now.toULong(), p.timestamp)
        }
    }

    @Test
    fun leave_isSignedLeaveWithTtl7() {
        val p = factory.leave(now)
        assertEquals(MessageType.LEAVE.value, p.type)
        assertEquals(7, p.ttl.toInt())
        assertEquals(0, p.payload.size)
        assertEquals(now.toULong(), p.timestamp)
        assertEquals(64, p.signature!!.size)
        assertTrue(verify(p))
    }

    @Test
    fun sign_resignsGossipPacket() {
        val unsigned = BitchatPacket(
            type = MessageType.REQUEST_SYNC.value,
            senderID = ByteArray(8),
            timestamp = now.toULong(),
            payload = byteArrayOf(1, 2, 3),
            ttl = 0u
        )
        assertTrue(verify(factory.sign(unsigned)))
    }

    private fun verify(p: BitchatPacket): Boolean =
        crypto.verifyEd25519Signature(p.signature!!, p.toBinaryDataForSigning()!!, crypto.getSigningPublicKey())
}
