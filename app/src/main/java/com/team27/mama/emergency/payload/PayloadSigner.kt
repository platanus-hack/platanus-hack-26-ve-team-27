package com.team27.mama.emergency.payload

import org.bouncycastle.crypto.params.Ed25519PrivateKeyParameters
import org.bouncycastle.crypto.signers.Ed25519Signer

/**
 * Signs with a device Ed25519 identity key. #56 implements it over bitchat's
 * `EncryptionService.getSigningPublicKey()` / `signData()`.
 */
interface PayloadSigner {
    /** 32 raw Ed25519 public key bytes. */
    val publicKey: ByteArray

    /** 64-byte pure Ed25519 (RFC 8032) signature over exactly [message]. */
    fun sign(message: ByteArray): ByteArray
}

/** BouncyCastle signer from a 32-byte seed; for tests and as a stand-in until #56. */
class Ed25519SeedSigner(seed: ByteArray) : PayloadSigner {
    private val privateKey = Ed25519PrivateKeyParameters(seed, 0)
    override val publicKey: ByteArray = privateKey.generatePublicKey().encoded

    override fun sign(message: ByteArray): ByteArray {
        val s = Ed25519Signer()
        s.init(true, privateKey)
        s.update(message, 0, message.size)
        return s.generateSignature()
    }
}
