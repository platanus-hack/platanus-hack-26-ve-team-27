package com.team27.mama.mesh.mesh

import com.team27.mama.mesh.model.PeerCapabilities
import com.team27.mama.mesh.model.RoutedPacket
import com.team27.mama.mesh.protocol.BitchatPacket
import com.team27.mama.mesh.protocol.MessageType
import com.team27.mama.mesh.protocol.SpecialRecipients
import kotlinx.coroutines.runBlocking
import org.junit.After
import org.junit.Assert.assertEquals
import org.junit.Assert.assertFalse
import org.junit.Assert.assertSame
import org.junit.Assert.assertTrue
import org.junit.Test

/** The trimmed MessageHandler only hands public MESSAGE text from verified peers to its delegate. */
class MessageHandlerAdaptedTest {

    private val myId = "0011223344556677"
    private val peerId = "8899aabbccddeeff"
    private val handler = MessageHandler(myId)
    private val delegate = FakeDelegate()

    init {
        handler.delegate = delegate
    }

    @After
    fun tearDown() = handler.shutdown()

    @Test
    fun broadcastFromVerifiedPeer_isDelivered() = runBlocking {
        delegate.peer = peerInfo(verified = true)
        val pkt = packet(MessageType.MESSAGE, SpecialRecipients.BROADCAST, "MAMA1:hola")
        handler.handleMessage(RoutedPacket(pkt, peerId))
        assertEquals(1, delegate.received.size)
        val (id, text, packet) = delegate.received.single()
        assertEquals(peerId, id)
        assertEquals("MAMA1:hola", text)
        assertSame(pkt, packet)
    }

    @Test
    fun messageWithoutRecipient_isTreatedAsBroadcast() = runBlocking {
        delegate.peer = peerInfo(verified = true)
        handler.handleMessage(RoutedPacket(packet(MessageType.MESSAGE, null, "MAMA1:x"), peerId))
        assertEquals(1, delegate.received.size)
    }

    @Test
    fun unknownPeer_isDropped() = runBlocking {
        delegate.peer = null
        handler.handleMessage(RoutedPacket(packet(MessageType.MESSAGE, SpecialRecipients.BROADCAST, "MAMA1:x"), peerId))
        assertTrue(delegate.received.isEmpty())
    }

    @Test
    fun unverifiedPeer_isDropped() = runBlocking {
        delegate.peer = peerInfo(verified = false)
        handler.handleMessage(RoutedPacket(packet(MessageType.MESSAGE, SpecialRecipients.BROADCAST, "MAMA1:x"), peerId))
        assertTrue(delegate.received.isEmpty())
    }

    @Test
    fun directedMessage_isDropped() = runBlocking {
        delegate.peer = peerInfo(verified = true)
        val toSomeone = ByteArray(8) { 0x11 }
        handler.handleMessage(RoutedPacket(packet(MessageType.MESSAGE, toSomeone, "MAMA1:x"), peerId))
        assertTrue(delegate.received.isEmpty())
    }

    @Test
    fun nonMessageType_isNotDelivered() = runBlocking {
        delegate.peer = peerInfo(verified = true)
        handler.handleMessage(RoutedPacket(packet(MessageType.VOICE_FRAME, SpecialRecipients.BROADCAST, "x"), peerId))
        handler.handleMessage(RoutedPacket(packet(MessageType.FILE_TRANSFER, SpecialRecipients.BROADCAST, "x"), peerId))
        assertTrue(delegate.received.isEmpty())
    }

    @Test
    fun ownMessage_isIgnored() = runBlocking {
        delegate.peer = peerInfo(verified = true)
        handler.handleMessage(RoutedPacket(packet(MessageType.MESSAGE, SpecialRecipients.BROADCAST, "MAMA1:x"), myId))
        assertTrue(delegate.received.isEmpty())
    }

    @Test
    fun leave_removesPeer_butChannelLeaveIsIgnored() = runBlocking {
        handler.handleLeave(RoutedPacket(packet(MessageType.LEAVE, null, "#canal"), peerId))
        assertTrue(delegate.removed.isEmpty())
        handler.handleLeave(RoutedPacket(packet(MessageType.LEAVE, null, ""), peerId))
        assertEquals(listOf(peerId), delegate.removed)
    }

    @Test
    fun publicVoiceFrame_fromVerifiedPeer_isAcceptedForRelay() {
        delegate.peer = peerInfo(verified = true)
        val pkt = packet(MessageType.VOICE_FRAME, SpecialRecipients.BROADCAST, "audio")
        assertTrue(handler.handlePublicVoiceFrame(RoutedPacket(pkt, peerId)))
        assertTrue(handler.handlePublicVoiceFrame(RoutedPacket(packet(MessageType.VOICE_FRAME, null, "a"), peerId)))
        assertTrue(handler.handlePublicVoiceFrame(RoutedPacket(pkt, myId)))
        assertTrue(delegate.received.isEmpty())
    }

    @Test
    fun publicVoiceFrame_failingUpstreamChecks_isRejected() {
        delegate.peer = peerInfo(verified = true)
        val now = System.currentTimeMillis()
        val directed = packet(MessageType.VOICE_FRAME, ByteArray(8) { 0x11 }, "a")
        val old = packet(MessageType.VOICE_FRAME, SpecialRecipients.BROADCAST, "a", now - 31_000)
        val future = packet(MessageType.VOICE_FRAME, SpecialRecipients.BROADCAST, "a", now + 31_000)
        assertFalse(handler.handlePublicVoiceFrame(RoutedPacket(directed, peerId)))
        assertFalse(handler.handlePublicVoiceFrame(RoutedPacket(old, peerId)))
        assertFalse(handler.handlePublicVoiceFrame(RoutedPacket(future, peerId)))
        val ok = packet(MessageType.VOICE_FRAME, SpecialRecipients.BROADCAST, "a")
        assertFalse(handler.handlePublicVoiceFrame(RoutedPacket(ok, null)))
        delegate.peer = peerInfo(verified = false)
        assertFalse(handler.handlePublicVoiceFrame(RoutedPacket(ok, peerId)))
        delegate.peer = null
        assertFalse(handler.handlePublicVoiceFrame(RoutedPacket(ok, peerId)))
    }

    private fun packet(
        type: MessageType,
        recipient: ByteArray?,
        text: String,
        timestampMs: Long = System.currentTimeMillis()
    ) = BitchatPacket(
        type = type.value,
        senderID = ByteArray(8),
        recipientID = recipient,
        timestamp = timestampMs.toULong(),
        payload = text.toByteArray(Charsets.UTF_8),
        ttl = 7u
    )

    private fun peerInfo(verified: Boolean) = PeerInfo(
        id = peerId,
        nickname = peerId,
        isConnected = true,
        isDirectConnection = true,
        noisePublicKey = null,
        signingPublicKey = null,
        isVerifiedNickname = verified,
        lastSeen = System.currentTimeMillis()
    )

    private class FakeDelegate : MessageHandlerDelegate {
        var peer: PeerInfo? = null
        val received = mutableListOf<Triple<String, String, BitchatPacket>>()
        val removed = mutableListOf<String>()

        override fun removePeer(peerID: String) {
            removed += peerID
        }
        override fun getPeerNickname(peerID: String): String? = peer?.nickname
        override fun getBroadcastRecipient(): ByteArray = SpecialRecipients.BROADCAST
        override fun getPeerInfo(peerID: String): PeerInfo? = peer?.takeIf { it.id == peerID }
        override fun updatePeerInfoFromVerifiedAnnouncement(
            peerID: String,
            nickname: String,
            noisePublicKey: ByteArray,
            signingPublicKey: ByteArray,
            isVerified: Boolean,
            capabilities: PeerCapabilities?
        ): Boolean = false
        override fun verifyEd25519Signature(signature: ByteArray, data: ByteArray, publicKey: ByteArray) = false
        override fun onPublicMessageReceived(peerID: String, content: String, packet: BitchatPacket) {
            received += Triple(peerID, content, packet)
        }
    }
}
