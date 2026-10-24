package com.team27.mama.mesh.crypto

import com.team27.mama.mesh.noise.NoisePeerIdentity
import org.junit.Assert.assertArrayEquals
import org.junit.Assert.assertEquals
import org.junit.Assert.assertFalse
import org.junit.Assert.assertTrue
import org.junit.Test

class EncryptionServiceTest {

    @Test
    fun signature_is64Bytes_andVerifies() {
        val (x, ed) = EncryptionService.generatePrivateKeys()
        val svc = EncryptionService(x, ed)
        val sig = svc.signData("hola".toByteArray())!!
        assertEquals(64, sig.size)
        assertTrue(svc.verifyEd25519Signature(sig, "hola".toByteArray(), svc.getSigningPublicKey()))
        assertFalse(svc.verifyEd25519Signature(sig, "hola!".toByteArray(), svc.getSigningPublicKey()))
    }

    @Test
    fun peerId_matchesUpstreamDerivation() {
        val (x, ed) = EncryptionService.generatePrivateKeys()
        val svc = EncryptionService(x, ed)
        assertEquals(NoisePeerIdentity.derivePeerID(svc.getStaticPublicKey()), svc.myPeerID)
        assertTrue(svc.myPeerID.matches(Regex("^[0-9a-f]{16}$")))
    }

    @Test
    fun sameKeyBytes_giveSameIdentity() {
        val (x, ed) = EncryptionService.generatePrivateKeys()
        assertArrayEquals(
            EncryptionService(x, ed).getSigningPublicKey(),
            EncryptionService(x.copyOf(), ed.copyOf()).getSigningPublicKey()
        )
        assertEquals(EncryptionService(x, ed).myPeerID, EncryptionService(x.copyOf(), ed.copyOf()).myPeerID)
    }
}
