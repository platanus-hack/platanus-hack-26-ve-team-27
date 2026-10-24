package com.team27.mama.mesh.mesh

import com.team27.mama.mesh.crypto.EncryptionService
import com.team27.mama.mesh.protocol.BitchatPacket
import com.team27.mama.mesh.protocol.MessageType
import com.team27.mama.mesh.protocol.SpecialRecipients
import org.junit.After
import org.junit.Assert.assertFalse
import org.junit.Assert.assertTrue
import org.junit.Test

/** The trimmed SecurityManager still enforces bitchat's signature and dedup rules. */
class SecurityManagerAdaptedTest {

    private val local = newService()
    private val remote = newService()
    private val remoteId = remote.myPeerID
    private val managers = mutableListOf<SecurityManager>()

    @After
    fun tearDown() {
        managers.forEach { it.shutdown() }
    }

    @Test
    fun signedFromKnownPeer_isAccepted() {
        val sm = newManager(knowsRemote = true)
        assertTrue(sm.validatePacket(signedBroadcast(remote), remoteId))
    }

    @Test
    fun duplicate_isRejectedSecondTime() {
        val sm = newManager(knowsRemote = true)
        val pkt = signedBroadcast(remote)
        assertTrue(sm.validatePacket(pkt, remoteId))
        assertFalse(sm.validatePacket(pkt, remoteId))
    }

    @Test
    fun unknownSigner_isRejected() {
        val sm = newManager(knowsRemote = false)
        assertFalse(sm.validatePacket(signedBroadcast(remote), remoteId))
    }

    @Test
    fun unsigned_isRejected() {
        val sm = newManager(knowsRemote = true)
        val pkt = signedBroadcast(remote).copy(signature = null)
        assertFalse(sm.validatePacket(pkt, remoteId))
    }

    @Test
    fun ownPacket_isRejected() {
        val sm = newManager(knowsRemote = true)
        assertFalse(sm.validatePacket(signedBroadcast(local), local.myPeerID))
    }

    @Test
    fun relayedTtlChange_keepsSignatureValid() {
        val pkt = signedBroadcast(remote)
        assertTrue(newManager(knowsRemote = true).validatePacket(pkt, remoteId))
        val relayed = pkt.copy(ttl = 3u)
        assertTrue(newManager(knowsRemote = true).validatePacket(relayed, remoteId))
    }

    private fun newService(): EncryptionService {
        val (x, ed) = EncryptionService.generatePrivateKeys()
        return EncryptionService(x, ed)
    }

    private fun newManager(knowsRemote: Boolean): SecurityManager {
        val sm = SecurityManager(local, local.myPeerID)
        sm.delegate = object : SecurityManagerDelegate {
            override fun getPeerInfo(peerID: String): PeerInfo? =
                if (knowsRemote && peerID == remoteId) {
                    PeerInfo(
                        id = remoteId,
                        nickname = remoteId,
                        isConnected = true,
                        isDirectConnection = true,
                        noisePublicKey = remote.getStaticPublicKey(),
                        signingPublicKey = remote.getSigningPublicKey(),
                        isVerifiedNickname = true,
                        lastSeen = System.currentTimeMillis()
                    )
                } else {
                    null
                }
        }
        managers += sm
        return sm
    }

    private fun signedBroadcast(sender: EncryptionService): BitchatPacket {
        val packet = BitchatPacket(
            type = MessageType.MESSAGE.value,
            ttl = 7u,
            senderID = sender.myPeerID,
            payload = "MAMA1:hola".toByteArray(Charsets.UTF_8)
        ).copy(recipientID = SpecialRecipients.BROADCAST)
        return packet.copy(signature = sender.signData(packet.toBinaryDataForSigning()!!))
    }
}
