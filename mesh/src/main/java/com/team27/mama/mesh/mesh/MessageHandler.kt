// Adapted from bitchat-android (https://github.com/permissionlesstech/bitchat-android)
// commit dfc67dad41691128bc6e7425bd69f2f409cb5bbf, file app/src/main/java/com/bitchat/android/mesh/MessageHandler.kt.
// GPL-3.0: see LICENSE and mesh/NOTICE.md. MAMÁ changes: package renamed to com.team27.mama.mesh;
// removed the Context parameter, packetProcessor, consecutiveDecryptFailures, handleNoiseEncrypted,
// registerDecryptFailure, sendDeliveryAck, handleNoiseHandshake,
// handlePrivateMessage, handleFavoriteNotificationFromMesh, the now-unused hexStringToByteArray, the
// meshgraph block in handleAnnounceWithResult and every voice, file and favorites branch.
// handleMessage drops updatePeerNickname and ignores directed messages. handleBroadcastMessage keeps
// the verified-peer guard and hands MESSAGE text to onPublicMessageReceived(peerID, text, packet)
// instead of building a BitchatMessage. handleLeave keeps peer removal and drops the channel callback
// (a '#channel' LEAVE is ignored). handlePublicVoiceFrame keeps upstream's acceptance checks and returns
// true instead of calling LiveVoiceManager, so public voice frames are relayed but never decoded or
// played. MessageHandlerDelegate trimmed to the members still called.

package com.team27.mama.mesh.mesh

import android.util.Log
import com.team27.mama.mesh.model.PeerCapabilities
import com.team27.mama.mesh.model.RoutedPacket
import com.team27.mama.mesh.protocol.BitchatPacket
import com.team27.mama.mesh.protocol.MessageType
import kotlinx.coroutines.*

sealed class AnnounceHandlingResult {
    data class Accepted(val isFirst: Boolean) : AnnounceHandlingResult()
    object Rejected : AnnounceHandlingResult()
}

/**
 * Handles processing of different message types
 * Extracted from BluetoothMeshService for better separation of concerns
 */
class MessageHandler(private val myPeerID: String) {
    
    companion object {
        private const val TAG = "MessageHandler"
        private const val ANNOUNCE_CLOCK_SKEW_TOLERANCE_MS = 10 * 60 * 1000L
    }

    // Delegate for callbacks
    var delegate: MessageHandlerDelegate? = null

    // Coroutines
    private val handlerScope = CoroutineScope(Dispatchers.IO + SupervisorJob())

    /**
     * Handle announce message with TLV decoding and signature verification - exactly like iOS
     */
    suspend fun handleAnnounce(routed: RoutedPacket): Boolean {
        return (handleAnnounceWithResult(routed) as? AnnounceHandlingResult.Accepted)?.isFirst ?: false
    }

    suspend fun handleAnnounceWithResult(routed: RoutedPacket): AnnounceHandlingResult {
        val packet = routed.packet
        val peerID = routed.peerID ?: "unknown"

        if (peerID == myPeerID) return AnnounceHandlingResult.Rejected

        // Peers use wall-clock packet timestamps; tolerate moderate device clock skew
        // during identity learning, or later signed messages cannot be verified.
        val now = System.currentTimeMillis()
        val clockSkewMs = kotlin.math.abs(now - packet.timestamp.toLong())
        if (clockSkewMs > ANNOUNCE_CLOCK_SKEW_TOLERANCE_MS) {
            Log.w(TAG, "Ignoring ANNOUNCE from ${peerID.take(8)} with excessive clock skew (${clockSkewMs}ms > ${ANNOUNCE_CLOCK_SKEW_TOLERANCE_MS}ms)")
            return AnnounceHandlingResult.Rejected
        }
        
        val announcement = AnnouncementIdentityValidator.verify(packet, peerID) { signature, data, key ->
            delegate?.verifyEd25519Signature(signature, data, key) ?: false
        }
        if (announcement == null) {
            Log.w(TAG, "Rejecting malformed, unbound, or invalidly signed ANNOUNCE from ${peerID.take(8)}")
            return AnnounceHandlingResult.Rejected
        }

        val persistedSigningKey = delegate?.getAuthenticatedSigningKey(announcement.noisePublicKey)
        if (persistedSigningKey != null &&
            !persistedSigningKey.contentEquals(announcement.signingPublicKey)
        ) {
            Log.w(TAG, "Rejecting ANNOUNCE Ed key that conflicts with authenticated peer state")
            return AnnounceHandlingResult.Rejected
        }

        var verified = true

        // Check for existing peer with different noise public key
        // If existing peer has a different noise public key, do not consider this verified
        val existingPeer = delegate?.getPeerInfo(peerID)
        
        if (existingPeer != null && existingPeer.noisePublicKey != null && !existingPeer.noisePublicKey!!.contentEquals(announcement.noisePublicKey)) {
            Log.w(TAG, "Announce key mismatch for ${peerID.take(8)} - keeping unverified")
            verified = false
        }

        if (
            existingPeer?.signingPublicKey != null &&
            !existingPeer.signingPublicKey!!.contentEquals(announcement.signingPublicKey)
        ) {
            Log.w(
                TAG,
                "Rejecting signing-key replacement for ${peerID.take(8)} without authenticated peer-state proof"
            )
            verified = false
        }

        // Require verified announce; ignore otherwise (no backward compatibility)
        if (!verified) {
            return AnnounceHandlingResult.Rejected
        }
        
        // Extract nickname and public keys from TLV data
        val nickname = announcement.nickname
        val noisePublicKey = announcement.noisePublicKey
        val signingPublicKey = announcement.signingPublicKey
        
        // Update peer info with verification status through new method
        val isFirstAnnounce = delegate?.updatePeerInfoFromVerifiedAnnouncement(
            peerID = peerID,
            nickname = nickname,
            noisePublicKey = noisePublicKey,
            signingPublicKey = signingPublicKey,
            isVerified = true,
            capabilities = announcement.capabilities
        ) ?: false

        Log.d(TAG, "Verified announce from $peerID (${announcement.nickname})")
        return AnnounceHandlingResult.Accepted(isFirstAnnounce)
    }
    
    /**
     * Handle broadcast or private message
     */
    suspend fun handleMessage(routed: RoutedPacket) {
        val packet = routed.packet
        val peerID = routed.peerID ?: "unknown"
        if (peerID == myPeerID) return
        val senderNickname = delegate?.getPeerNickname(peerID)
        if (senderNickname != null) {
            Log.d(TAG, "Received message from $senderNickname")
        }
        
        val recipientID = packet.recipientID?.takeIf { !it.contentEquals(delegate?.getBroadcastRecipient()) }
        
        if (recipientID == null) {
            // BROADCAST MESSAGE
            handleBroadcastMessage(routed)
        }
        // Message relay is now handled by centralized PacketRelayManager
    }

    /**
     * Upstream's acceptance checks for a public VOICE_FRAME; true lets PacketProcessor relay it.
     * MAMÁ does not decode or play the audio.
     */
    fun handlePublicVoiceFrame(routed: RoutedPacket): Boolean {
        val packet = routed.packet
        val peerID = routed.peerID ?: return false
        if (peerID == myPeerID) return true
        val recipient = packet.recipientID
        if (recipient != null && !recipient.contentEquals(delegate?.getBroadcastRecipient())) return false
        if (packet.timestamp > Long.MAX_VALUE.toULong()) return false
        val ageMs = System.currentTimeMillis() - packet.timestamp.toLong()
        if (ageMs !in -30_000L..30_000L) return false
        val peerInfo = delegate?.getPeerInfo(peerID)
        if (peerInfo == null || !peerInfo.isVerifiedNickname) return false
        return true
    }

    /**
     * Handle broadcast message with verification enforcement
     */
    private suspend fun handleBroadcastMessage(routed: RoutedPacket) {
        val packet = routed.packet
        val peerID = routed.peerID ?: "unknown"
        
        // Enforce: only accept public messages from verified peers we know
        val peerInfo = delegate?.getPeerInfo(peerID)
        if (peerInfo == null || !peerInfo.isVerifiedNickname) {
            Log.w(TAG, "Dropping public message from unverified peer ${peerID.take(8)}")
            return
        }
        
        try {
            if (MessageType.fromValue(packet.type) == MessageType.MESSAGE) {
                delegate?.onPublicMessageReceived(peerID, String(packet.payload, Charsets.UTF_8), packet)
            }
        } catch (e: Exception) {
            Log.e(TAG, "Failed to process broadcast message: ${e.message}")
        }
    }
    
    /**
     * Handle leave message
     */
    suspend fun handleLeave(routed: RoutedPacket) {
        val packet = routed.packet
        val peerID = routed.peerID ?: "unknown"
        val content = String(packet.payload, Charsets.UTF_8)
        
        if (!content.startsWith("#")) {
            // Peer disconnect (channel leaves are ignored: MAMÁ has no channels)
            delegate?.removePeer(peerID)
        }
        
        // Leave message relay is now handled by centralized PacketRelayManager
    }
    
    /**
     * Get debug information
     */
    fun getDebugInfo(): String {
        return buildString {
            appendLine("=== Message Handler Debug Info ===")
            appendLine("Handler Scope Active: ${handlerScope.isActive}")
            appendLine("My Peer ID: $myPeerID")
        }
    }
    
    /**
     * Shutdown the handler
     */
    fun shutdown() {
        handlerScope.cancel()
    }
}

/**
 * Delegate interface for message handler callbacks
 */
interface MessageHandlerDelegate {
    fun removePeer(peerID: String)
    fun getPeerNickname(peerID: String): String?
    fun getBroadcastRecipient(): ByteArray
    fun getPeerInfo(peerID: String): PeerInfo?
    fun updatePeerInfoFromVerifiedAnnouncement(
        peerID: String,
        nickname: String,
        noisePublicKey: ByteArray,
        signingPublicKey: ByteArray,
        isVerified: Boolean,
        capabilities: PeerCapabilities? = null
    ): Boolean
    fun verifyEd25519Signature(signature: ByteArray, data: ByteArray, publicKey: ByteArray): Boolean
    fun getAuthenticatedSigningKey(noisePublicKey: ByteArray): ByteArray? = null
    fun onPublicMessageReceived(peerID: String, content: String, packet: BitchatPacket)
}
