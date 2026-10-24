package com.team27.mama.emergency.payload

import java.security.GeneralSecurityException
import java.security.SecureRandom
import javax.crypto.Cipher
import javax.crypto.spec.GCMParameterSpec
import javax.crypto.spec.SecretKeySpec
import org.bouncycastle.crypto.digests.SHA256Digest
import org.bouncycastle.crypto.generators.HKDFBytesGenerator
import org.bouncycastle.crypto.params.HKDFParameters
import org.bouncycastle.crypto.params.X25519PrivateKeyParameters
import org.bouncycastle.crypto.params.X25519PublicKeyParameters

/**
 * Anonymous sealed box: ephemeral X25519 public key (32) || AES-256-GCM
 * ciphertext || tag (16). Byte-identical to the Go package.
 */
object SealedBox {
    const val OVERHEAD = 48
    const val INFO = "MAMA-sealed-box-v1"

    fun seal(
        plaintext: ByteArray,
        recipientPublic: ByteArray,
        random: SecureRandom = SecureRandom(),
    ): ByteArray {
        require(recipientPublic.size == 32) { "recipient public key must be 32 bytes" }
        val eph = ByteArray(32)
        random.nextBytes(eph)
        return sealWith(plaintext, recipientPublic, eph)
    }

    internal fun sealWith(
        plaintext: ByteArray,
        recipientPublic: ByteArray,
        ephemeralPrivate: ByteArray,
    ): ByteArray {
        require(recipientPublic.size == 32) { "recipient public key must be 32 bytes" }
        require(ephemeralPrivate.size == 32) { "ephemeral private key must be 32 bytes" }
        val eph = X25519PrivateKeyParameters(ephemeralPrivate, 0)
        val ephPub = eph.generatePublicKey().encoded
        val shared = agree(eph, recipientPublic)
            ?: throw IllegalArgumentException("key agreement produced an all-zero secret")
        val (key, nonce) = deriveKeyNonce(shared, ephPub, recipientPublic)
        return ephPub + gcm(Cipher.ENCRYPT_MODE, key, nonce, plaintext)
    }

    fun open(box: ByteArray, recipientPrivate: ByteArray): ByteArray {
        if (box.size < OVERHEAD) {
            throw PayloadException(
                PayloadException.Reason.FORMAT,
                "sealed box is ${box.size} bytes, want at least $OVERHEAD",
            )
        }
        require(recipientPrivate.size == 32) { "recipient private key must be 32 bytes" }
        val priv = X25519PrivateKeyParameters(recipientPrivate, 0)
        val ephPub = box.copyOfRange(0, 32)
        val shared = agree(priv, ephPub)
            ?: throw PayloadException(PayloadException.Reason.DECRYPT, "key agreement failed")
        val recipientPub = priv.generatePublicKey().encoded
        val (key, nonce) = deriveKeyNonce(shared, ephPub, recipientPub)
        try {
            return gcm(Cipher.DECRYPT_MODE, key, nonce, box.copyOfRange(32, box.size))
        } catch (e: GeneralSecurityException) {
            throw PayloadException(PayloadException.Reason.DECRYPT, "authentication failed", e)
        }
    }

    fun publicKeyOf(x25519Private: ByteArray): ByteArray {
        require(x25519Private.size == 32) { "private key must be 32 bytes" }
        return X25519PrivateKeyParameters(x25519Private, 0).generatePublicKey().encoded
    }

    /** Returns null when the shared secret is all zero (low-order point). */
    private fun agree(priv: X25519PrivateKeyParameters, publicBytes: ByteArray): ByteArray? {
        val out = ByteArray(32)
        try {
            priv.generateSecret(X25519PublicKeyParameters(publicBytes, 0), out, 0)
        } catch (e: IllegalStateException) {
            return null
        }
        return out
    }

    private fun deriveKeyNonce(
        shared: ByteArray,
        ephPub: ByteArray,
        recipientPub: ByteArray,
    ): Pair<ByteArray, ByteArray> {
        val hkdf = HKDFBytesGenerator(SHA256Digest())
        hkdf.init(HKDFParameters(shared, ephPub + recipientPub, INFO.toByteArray(Charsets.US_ASCII)))
        val okm = ByteArray(44)
        hkdf.generateBytes(okm, 0, okm.size)
        return okm.copyOfRange(0, 32) to okm.copyOfRange(32, 44)
    }

    private fun gcm(mode: Int, key: ByteArray, nonce: ByteArray, input: ByteArray): ByteArray {
        val cipher = Cipher.getInstance("AES/GCM/NoPadding")
        cipher.init(mode, SecretKeySpec(key, "AES"), GCMParameterSpec(128, nonce))
        return cipher.doFinal(input)
    }
}
