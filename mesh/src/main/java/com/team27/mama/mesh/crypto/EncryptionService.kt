// Derived from bitchat-android crypto/EncryptionService.kt @ dfc67da (GPL-3.0).
// Upstream: https://github.com/permissionlesstech/bitchat-android commit
// dfc67dad41691128bc6e7425bd69f2f409cb5bbf, file app/src/main/java/com/bitchat/android/crypto/EncryptionService.kt.
// GPL-3.0: see LICENSE and mesh/NOTICE.md.
// MAMÁ changes: Noise sessions removed; keeps the Ed25519 signing key and an X25519 static key for
// the peer ID; key bytes are injected (storage in MeshKeyStore).

package com.team27.mama.mesh.crypto

import android.util.Log
import org.bouncycastle.crypto.generators.Ed25519KeyPairGenerator
import org.bouncycastle.crypto.generators.X25519KeyPairGenerator
import org.bouncycastle.crypto.params.Ed25519KeyGenerationParameters
import org.bouncycastle.crypto.params.Ed25519PrivateKeyParameters
import org.bouncycastle.crypto.params.Ed25519PublicKeyParameters
import org.bouncycastle.crypto.params.X25519KeyGenerationParameters
import org.bouncycastle.crypto.params.X25519PrivateKeyParameters
import org.bouncycastle.crypto.signers.Ed25519Signer
import java.security.MessageDigest
import java.security.SecureRandom

/**
 * Holds this node's two long-lived keys: the X25519 static key, whose public half defines the
 * bitchat peer ID, and the Ed25519 key that signs packets and announcements.
 */
class EncryptionService(staticPrivateKey: ByteArray, signingPrivateKey: ByteArray) {

    companion object {
        private const val TAG = "EncryptionService"
        const val KEY_SIZE = 32

        /** Returns fresh (X25519 private, Ed25519 private) keys, 32 bytes each. */
        fun generatePrivateKeys(random: SecureRandom = SecureRandom()): Pair<ByteArray, ByteArray> {
            val x25519 = X25519KeyPairGenerator().apply { init(X25519KeyGenerationParameters(random)) }
                .generateKeyPair().private as X25519PrivateKeyParameters
            val ed25519 = Ed25519KeyPairGenerator().apply { init(Ed25519KeyGenerationParameters(random)) }
                .generateKeyPair().private as Ed25519PrivateKeyParameters
            return x25519.encoded to ed25519.encoded
        }
    }

    private val staticPublicKey: ByteArray
    private val ed25519PrivateKey: Ed25519PrivateKeyParameters
    private val ed25519PublicKey: Ed25519PublicKeyParameters

    init {
        require(staticPrivateKey.size == KEY_SIZE) { "X25519 private key must be $KEY_SIZE bytes" }
        require(signingPrivateKey.size == KEY_SIZE) { "Ed25519 private key must be $KEY_SIZE bytes" }
        staticPublicKey = X25519PrivateKeyParameters(staticPrivateKey, 0).generatePublicKey().encoded
        ed25519PrivateKey = Ed25519PrivateKeyParameters(signingPrivateKey, 0)
        ed25519PublicKey = ed25519PrivateKey.generatePublicKey()
    }

    /** First 8 bytes of SHA-256(static public key) as 16 lowercase hex characters. */
    val myPeerID: String = getIdentityFingerprint().take(16)

    /** Our X25519 static public key (32 bytes). */
    fun getStaticPublicKey(): ByteArray = staticPublicKey.copyOf()

    /** Our Ed25519 signing public key (32 bytes). */
    fun getSigningPublicKey(): ByteArray = ed25519PublicKey.encoded

    /** Sign data with our Ed25519 key; returns the 64-byte signature, or null on failure. */
    fun signData(data: ByteArray): ByteArray? {
        return try {
            val signer = Ed25519Signer()
            signer.init(true, ed25519PrivateKey)
            signer.update(data, 0, data.size)
            signer.generateSignature()
        } catch (e: Exception) {
            Log.e(TAG, "Failed to sign data with Ed25519: ${e.message}")
            null
        }
    }

    /** Verify an Ed25519 signature over data with the given public key. */
    fun verifyEd25519Signature(signature: ByteArray, data: ByteArray, publicKeyBytes: ByteArray): Boolean {
        return try {
            val publicKey = Ed25519PublicKeyParameters(publicKeyBytes, 0)
            val verifier = Ed25519Signer()
            verifier.init(false, publicKey)
            verifier.update(data, 0, data.size)
            verifier.verifySignature(signature)
        } catch (e: Exception) {
            Log.e(TAG, "Failed to verify Ed25519 signature: ${e.message}")
            false
        }
    }

    /** SHA-256 of the static public key as 64 lowercase hex characters. */
    fun getIdentityFingerprint(): String =
        MessageDigest.getInstance("SHA-256")
            .digest(staticPublicKey)
            .joinToString("") { "%02x".format(it) }
}
