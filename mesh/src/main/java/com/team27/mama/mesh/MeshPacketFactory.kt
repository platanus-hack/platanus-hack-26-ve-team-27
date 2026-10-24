// Derived from bitchat-android mesh/BluetoothMeshService.kt @ dfc67da (GPL-3.0).
// Upstream: https://github.com/permissionlesstech/bitchat-android commit
// dfc67dad41691128bc6e7425bd69f2f409cb5bbf, file app/src/main/java/com/bitchat/android/mesh/BluetoothMeshService.kt
// (sendMessage, sendBroadcastAnnounce, sendLeaveAnnouncement, signPacketBeforeBroadcast, hexStringToByteArray).
// GPL-3.0: see LICENSE and mesh/NOTICE.md.
// MAMÁ changes: pure packet building, the caller supplies the timestamp; no source routing; no
// gossip-neighbor TLV; nickname = our peer ID, capabilities = NONE; signing failure throws instead of
// sending unsigned; broadcastSequence puts an ANNOUNCE before every MESSAGE.

package com.team27.mama.mesh

import com.team27.mama.mesh.crypto.EncryptionService
import com.team27.mama.mesh.model.IdentityAnnouncement
import com.team27.mama.mesh.model.PeerCapabilities
import com.team27.mama.mesh.protocol.BitchatPacket
import com.team27.mama.mesh.protocol.MessageType
import com.team27.mama.mesh.protocol.SpecialRecipients
import com.team27.mama.mesh.util.AppConstants

/** Builds the signed packets this node originates. No I/O; every packet is Ed25519-signed. */
internal class MeshPacketFactory(private val crypto: EncryptionService) {

    val myPeerID: String = crypto.myPeerID

    private val senderID: ByteArray = hexStringToByteArray(myPeerID)

    /** Signed ANNOUNCE carrying our identity (TLV), so peers learn our signing key. */
    fun announce(nowMs: Long): BitchatPacket {
        val payload = IdentityAnnouncement(
            nickname = myPeerID,
            noisePublicKey = crypto.getStaticPublicKey(),
            signingPublicKey = crypto.getSigningPublicKey(),
            capabilities = PeerCapabilities.NONE
        ).encode()!!
        return sign(packet(MessageType.ANNOUNCE, recipientID = null, payload = payload, nowMs = nowMs))
    }

    /** Signed public MESSAGE to every peer. */
    fun broadcastMessage(text: String, nowMs: Long): BitchatPacket = sign(
        packet(
            MessageType.MESSAGE,
            recipientID = SpecialRecipients.BROADCAST,
            payload = text.toByteArray(Charsets.UTF_8),
            nowMs = nowMs
        )
    )

    /** Signed LEAVE with an empty payload (peer disconnect, not a channel leave). */
    fun leave(nowMs: Long): BitchatPacket =
        sign(packet(MessageType.LEAVE, recipientID = null, payload = byteArrayOf(), nowMs = nowMs))

    /**
     * ANNOUNCE then MESSAGE, same timestamp. A peer that has not seen our ANNOUNCE drops our
     * MESSAGE without relaying it, so every broadcast carries a fresh one first.
     */
    fun broadcastSequence(text: String, nowMs: Long): List<BitchatPacket> =
        listOf(announce(nowMs), broadcastMessage(text, nowMs))

    /** Ed25519 over the canonical packet (TTL 0, no signature). Also re-signs gossip packets. */
    fun sign(packet: BitchatPacket): BitchatPacket {
        val data = packet.toBinaryDataForSigning()
            ?: throw IllegalStateException("Failed to encode packet type ${packet.type} for signing")
        val signature = crypto.signData(data)
            ?: throw IllegalStateException("Failed to sign packet type ${packet.type}")
        return packet.copy(signature = signature)
    }

    private fun packet(type: MessageType, recipientID: ByteArray?, payload: ByteArray, nowMs: Long) =
        BitchatPacket(
            version = 1u,
            type = type.value,
            senderID = senderID.copyOf(),
            recipientID = recipientID?.copyOf(),
            timestamp = nowMs.toULong(),
            payload = payload,
            signature = null,
            ttl = AppConstants.MESSAGE_TTL_HOPS
        )

    /** Hex peer ID to its 8-byte wire form, as upstream (zero-padded). */
    private fun hexStringToByteArray(hexString: String): ByteArray {
        val result = ByteArray(8) { 0 }
        var tempID = hexString
        var index = 0
        while (tempID.length >= 2 && index < 8) {
            val byte = tempID.substring(0, 2).toIntOrNull(16)?.toByte()
            if (byte != null) result[index] = byte
            tempID = tempID.substring(2)
            index++
        }
        return result
    }
}
