package com.team27.mama.emergency

import com.team27.mama.emergency.payload.Ed25519SeedSigner
import com.team27.mama.emergency.payload.Kind
import com.team27.mama.emergency.payload.PayloadCodec
import com.team27.mama.mesh.MeshIdentity
import org.junit.Assert.assertEquals
import org.junit.Assert.assertThrows
import org.junit.Test

class MeshIdentitySignerTest {
    private val backing = Ed25519SeedSigner(ByteArray(32) { 1 })
    private val fake = object : MeshIdentity {
        override val peerId = "0123456789abcdef"
        override val publicKey: ByteArray get() = backing.publicKey
        override fun sign(data: ByteArray) = backing.sign(data)
    }

    @Test
    fun signsVerifiablePayloads() {
        val now = 1_790_000_000_000L
        val frame = EmergencyFrames.build(Kind.ALERT, MeshIdentitySigner(fake), 5L, 0, null, now)
        assertEquals(frame.payload, PayloadCodec.verify(frame.signed, fake.publicKey))
    }

    @Test
    fun identityFailureBecomesMeshUnavailable() {
        val broken = object : MeshIdentity {
            override val peerId: String get() = error("keystore")
            override val publicKey: ByteArray get() = error("keystore")
            override fun sign(data: ByteArray): ByteArray = error("keystore")
        }
        val signer = MeshIdentitySigner(broken)
        assertThrows(MeshUnavailableException::class.java) { signer.publicKey }
        assertThrows(MeshUnavailableException::class.java) { signer.sign(ByteArray(1)) }
    }
}
