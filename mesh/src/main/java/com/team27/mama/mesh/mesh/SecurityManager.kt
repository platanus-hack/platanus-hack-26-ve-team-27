// Adapted from bitchat-android (https://github.com/permissionlesstech/bitchat-android)
// commit dfc67dad41691128bc6e7425bd69f2f409cb5bbf, file app/src/main/java/com/bitchat/android/mesh/SecurityManager.kt.
// GPL-3.0: see LICENSE and mesh/NOTICE.md. MAMÁ changes: package renamed to com.team27.mama.mesh;
// Noise removed: deleted handleNoiseHandshake, signPacket, both encryptForPeer, decryptFromPeer,
// getCombinedPublicKeyData and hasKeysForPeer, their imports (noise.*, RoutedPacket, toHexString), and
// onKeyExchangeCompleted/sendHandshakeResponse from SecurityManagerDelegate. validatePacket,
// verifyPacketSignature, dedup and cleanup are unchanged.

package com.team27.mama.mesh.mesh

import android.util.Log
import com.team27.mama.mesh.crypto.EncryptionService
import com.team27.mama.mesh.protocol.BitchatPacket
import com.team27.mama.mesh.sync.PacketIdUtil
import com.team27.mama.mesh.protocol.MessageType
import kotlinx.coroutines.*
import java.util.*
import kotlin.collections.mutableSetOf

/**
 * Manages security aspects of the mesh network including duplicate detection,
 * replay attack protection, and key exchange handling
 * Extracted from BluetoothMeshService for better separation of concerns
 */
class SecurityManager(private val encryptionService: EncryptionService, private val myPeerID: String) {
    
    companion object {
        private const val TAG = "SecurityManager"
        private const val MESSAGE_TIMEOUT = com.team27.mama.mesh.util.AppConstants.Security.MESSAGE_TIMEOUT_MS // 5 minutes (same as iOS)
        private const val CLEANUP_INTERVAL = com.team27.mama.mesh.util.AppConstants.Security.CLEANUP_INTERVAL_MS // 5 minutes
        private const val MAX_PROCESSED_MESSAGES = com.team27.mama.mesh.util.AppConstants.Security.MAX_PROCESSED_MESSAGES
        private const val MAX_PROCESSED_KEY_EXCHANGES = com.team27.mama.mesh.util.AppConstants.Security.MAX_PROCESSED_KEY_EXCHANGES
        private const val KEY_EXCHANGE_DEDUP_TIMEOUT = com.team27.mama.mesh.util.AppConstants.Security.KEY_EXCHANGE_DEDUP_TIMEOUT_MS
    }
    
    // Security tracking
    private val processedMessages = Collections.synchronizedSet(mutableSetOf<String>())
    private val processedKeyExchanges = Collections.synchronizedSet(mutableSetOf<String>())
    private val messageTimestamps = Collections.synchronizedMap(mutableMapOf<String, Long>())
    private val keyExchangeTimestamps = Collections.synchronizedMap(mutableMapOf<String, Long>())
    
    // Delegate for callbacks
    var delegate: SecurityManagerDelegate? = null
    
    // Coroutines
    private val managerScope = CoroutineScope(Dispatchers.IO + SupervisorJob())
    
    init {
        startPeriodicCleanup()
    }
    
    /**
     * Validate packet security (timestamp, replay attacks, duplicates, signatures)
     */
    fun validatePacket(packet: BitchatPacket, peerID: String): Boolean {
        // Skip validation for our own packets
        if (peerID == myPeerID) {
            return false
        }
        
        // Replay attack protection (same 5-minute window as iOS)
        val currentTime = System.currentTimeMillis()
        val messageType = MessageType.fromValue(packet.type)

        // LEAVE mutates presence immediately and cannot be safely replayed after the in-memory
        // duplicate cache expires (or after an app restart). Bound it to the same five-minute
        // security window used for duplicate retention while tolerating symmetric clock skew.
        if (messageType == MessageType.LEAVE) {
            val now = currentTime.coerceAtLeast(0).toULong()
            val clockSkew = if (packet.timestamp >= now) {
                packet.timestamp - now
            } else {
                now - packet.timestamp
            }
            if (clockSkew > MESSAGE_TIMEOUT.toULong()) {
                Log.w(TAG, "Dropping stale or future-dated LEAVE from $peerID")
                return false
            }
        }

        // Duplicate detection
        val messageID = generateMessageID(packet, peerID)
        
        if (processedMessages.contains(messageID)) {
            // Check for ANNOUNCE exception: allow if it looks like a direct neighbor (max TTL)
            // This ensures we observe the same peer on a new direct transport connection,
            // while still dropping looped/relayed duplicates.
            val isFreshAnnounce = messageType == MessageType.ANNOUNCE &&
                    packet.ttl >= com.team27.mama.mesh.util.AppConstants.MESSAGE_TTL_HOPS

            if (!isFreshAnnounce) {
                return false
            }
        }

        // Enforce mandatory signature verification
        if (!verifyPacketSignature(packet, peerID)) {
            return false
        }

        // Record only authenticated packets. Recording an attacker-controlled
        // invalid packet first would let it poison duplicate detection for a
        // later legitimate packet with the same timestamp and payload.
        processedMessages.add(messageID)
        messageTimestamps[messageID] = currentTime

        return true
    }
    
    /**
     * Verify a packet signature against the signing key learned from the
     * peer's verified announcement. Signatures cover the canonical packet,
     * not only its payload; otherwise routing and recipient fields could be
     * changed without invalidating the signature.
     */
    fun verifySignature(packet: BitchatPacket, peerID: String): Boolean {
        return verifyPacketSignature(packet, peerID)
    }
    
    /** Deduplicates by peer and a hash of packet type, sender, timestamp, and full payload. */
    private fun generateMessageID(packet: BitchatPacket, peerID: String): String {
        return "$peerID-${PacketIdUtil.computeIdHex(packet)}"
    }
    
    /**
     * Verify packet signature using peer's signing public key
     * Returns true only if signature is present and valid
     */
    private fun verifyPacketSignature(packet: BitchatPacket, peerID: String): Boolean {
        try {
            // Public packets that mutate identity, presence, or user-visible state must prove the
            // signing key learned from a verified announcement. LEAVE is included so an attacker
            // cannot evict a claimed peer or amplify a forged departure through relay.
            if (MessageType.fromValue(packet.type) !in setOf(
                    MessageType.ANNOUNCE,
                    MessageType.MESSAGE,
                    MessageType.FILE_TRANSFER,
                    MessageType.VOICE_FRAME,
                    MessageType.LEAVE
                )) {
                return true
            }

            if (MessageType.fromValue(packet.type) == MessageType.ANNOUNCE) {
                val announcement = AnnouncementIdentityValidator.verify(packet, peerID) { signature, data, key ->
                    encryptionService.verifyEd25519Signature(signature, data, key)
                } ?: run {
                    Log.w(TAG, "Rejecting malformed, unbound, or invalidly signed ANNOUNCE from $peerID")
                    return false
                }

                val persistedSigningKey = delegate?.getAuthenticatedSigningKey(announcement.noisePublicKey)
                if (persistedSigningKey != null &&
                    !persistedSigningKey.contentEquals(announcement.signingPublicKey)
                ) {
                    Log.w(TAG, "Rejecting ANNOUNCE Ed key that conflicts with authenticated peer state for $peerID")
                    return false
                }

                val existingPeer = delegate?.getPeerInfo(peerID)
                if (
                    existingPeer?.noisePublicKey != null &&
                    !existingPeer.noisePublicKey!!.contentEquals(announcement.noisePublicKey)
                ) {
                    Log.w(TAG, "Rejecting ANNOUNCE Noise-key replacement for $peerID")
                    return false
                }
                if (
                    existingPeer?.signingPublicKey != null &&
                    !existingPeer.signingPublicKey!!.contentEquals(announcement.signingPublicKey)
                ) {
                    Log.w(TAG, "Rejecting ANNOUNCE signing-key replacement without authenticated peer state for $peerID")
                    return false
                }
                return true
            }

            // 1. Mandatory Signature Check
            if (packet.signature == null) {
                Log.w(TAG, "Signature check for $peerID: NO_SIGNATURE (packet type ${packet.type})")
                return false
            }
            
            // 2. Get Signing Public Key
            val peerInfo = delegate?.getPeerInfo(peerID)
            val signingPublicKey = peerInfo?.signingPublicKey
            
            if (signingPublicKey == null) {
                // If we don't have a key (and it's not an announce), we can't verify.
                // For security, we must reject packets from unknown peers unless it's an announce.
                Log.w(TAG, "Signature check for $peerID: NO_SIGNING_KEY_AVAILABLE (packet type ${packet.type})")
                return false
            }
            
            // 3. Get Canonical Data
            val packetDataForSigning = packet.toBinaryDataForSigning()
            if (packetDataForSigning == null) {
                Log.w(TAG, "Signature check for $peerID: ENCODING_ERROR (packet type ${packet.type})")
                return false
            }
            
            // 4. Verify Signature
            val signature = packet.signature!!
            val isSignatureValid = encryptionService.verifyEd25519Signature(
                signature,
                packetDataForSigning,
                signingPublicKey
            )
            
            if (isSignatureValid) {
                return true
            } else {
                Log.w(TAG, "Signature INVALID for $peerID (type ${packet.type})")
                return false
            }

        } catch (e: Exception) {
            Log.e(TAG, "Signature verification error for $peerID: ${e.message}")
            return false
        }
    }
    
    /**
     * Get debug information
     */
    fun getDebugInfo(): String {
        return buildString {
            appendLine("=== Security Manager Debug Info ===")
            appendLine("Processed Messages: ${processedMessages.size}")
            appendLine("Processed Key Exchanges: ${processedKeyExchanges.size}")
            appendLine("Message Timestamps: ${messageTimestamps.size}")
            
            if (processedKeyExchanges.isNotEmpty()) {
                appendLine("Key Exchange History:")
                processedKeyExchanges.take(10).forEach { exchange ->
                    appendLine("  - $exchange")
                }
                if (processedKeyExchanges.size > 10) {
                    appendLine("  ... and ${processedKeyExchanges.size - 10} more")
                }
            }
        }
    }
    
    /**
     * Start periodic cleanup
     */
    private fun startPeriodicCleanup() {
        managerScope.launch {
            while (isActive) {
                delay(CLEANUP_INTERVAL)
                cleanupOldData()
            }
        }
    }
    
    /**
     * Clean up old processed messages and timestamps
     */
    internal fun cleanupOldData(nowMs: Long = System.currentTimeMillis()) {
        val cutoffTime = nowMs - MESSAGE_TIMEOUT

        // Clean up old message timestamps and corresponding processed messages
        val messagesToRemove = messageTimestamps.entries.filter { (_, timestamp) ->
            timestamp < cutoffTime
        }.map { it.key }

        messagesToRemove.forEach { messageId ->
            messageTimestamps.remove(messageId)
            processedMessages.remove(messageId)
        }

        // Limit the size of processed messages set
        if (processedMessages.size > MAX_PROCESSED_MESSAGES) {
            val excess = processedMessages.size - MAX_PROCESSED_MESSAGES
            val toRemove = processedMessages.take(excess)
            processedMessages.removeAll(toRemove.toSet())
            removeFromMessageTimestamps(toRemove)
        }

        // Expire handshake dedup entries by time so a delayed same-ephemeral delivery
        // (e.g. a re-handshake retry after a failed attempt) is not blocked forever.
        val keyExchangeCutoff = nowMs - KEY_EXCHANGE_DEDUP_TIMEOUT
        val keyExchangesToRemove = keyExchangeTimestamps.entries.filter { (_, timestamp) ->
            timestamp < keyExchangeCutoff
        }.map { it.key }

        keyExchangesToRemove.forEach { exchangeKey ->
            keyExchangeTimestamps.remove(exchangeKey)
            processedKeyExchanges.remove(exchangeKey)
        }

        // Limit the size of processed key exchanges set
        if (processedKeyExchanges.size > MAX_PROCESSED_KEY_EXCHANGES) {
            val excess = processedKeyExchanges.size - MAX_PROCESSED_KEY_EXCHANGES
            val toRemove = processedKeyExchanges.take(excess)
            processedKeyExchanges.removeAll(toRemove.toSet())
            toRemove.forEach { keyExchangeTimestamps.remove(it) }
        }
    }
    
    /**
     * Helper to remove entries from messageTimestamps
     */
    private fun removeFromMessageTimestamps(messageIds: List<String>) {
        messageIds.forEach { messageId ->
            messageTimestamps.remove(messageId)
        }
    }
    
    /**
     * Clear all security data
     */
    fun clearAllData() {
        processedMessages.clear()
        processedKeyExchanges.clear()
        messageTimestamps.clear()
        keyExchangeTimestamps.clear()
    }
    
    /**
     * Shutdown the manager
     */
    fun shutdown() {
        managerScope.cancel()
        clearAllData()
    }
}

/**
 * Delegate interface for security manager callbacks
 */
interface SecurityManagerDelegate {
    fun getPeerInfo(peerID: String): PeerInfo? // NEW: For signature verification
    fun getAuthenticatedSigningKey(noisePublicKey: ByteArray): ByteArray? = null
}
