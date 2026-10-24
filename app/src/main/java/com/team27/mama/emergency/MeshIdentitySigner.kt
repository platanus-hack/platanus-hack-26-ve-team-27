package com.team27.mama.emergency

import com.team27.mama.emergency.payload.PayloadSigner
import com.team27.mama.mesh.MeshGateway
import com.team27.mama.mesh.MeshIdentity

/** The mesh identity (Android Keystore) could not be read, so the device cannot sign. */
class MeshUnavailableException(cause: Throwable) : Exception("Mesh identity unavailable", cause)

/**
 * Signs with the bitchat Ed25519 identity. Any failure reading the identity or signing
 * surfaces as [MeshUnavailableException], never as a raw crash.
 */
class MeshIdentitySigner(private val identity: MeshIdentity) : PayloadSigner {
    override val publicKey: ByteArray
        get() = guard { identity.publicKey }

    override fun sign(message: ByteArray): ByteArray = guard { identity.sign(message) }

    companion object {
        /** Reading [MeshGateway.identity] throws when the key store is unusable. */
        fun of(gateway: MeshGateway): MeshIdentitySigner =
            MeshIdentitySigner(guard { gateway.identity })

        /**
         * A signer that reads [MeshGateway.identity] on every use, so building it never throws:
         * a broken key store surfaces as [MeshUnavailableException] from `publicKey`/`sign`.
         */
        fun deferred(gateway: MeshGateway): PayloadSigner = object : PayloadSigner {
            override val publicKey: ByteArray get() = of(gateway).publicKey

            override fun sign(message: ByteArray): ByteArray = of(gateway).sign(message)
        }

        private inline fun <T> guard(block: () -> T): T =
            try {
                block()
            } catch (e: Exception) {
                throw MeshUnavailableException(e)
            }
    }
}
