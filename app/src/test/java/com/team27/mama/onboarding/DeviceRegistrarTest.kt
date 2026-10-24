package com.team27.mama.onboarding

import com.team27.mama.contract.DeviceRegistration
import com.team27.mama.contract.RemoteConfig
import com.team27.mama.emergency.payload.Ed25519SeedSigner
import com.team27.mama.server.ServerApi
import com.team27.mama.server.ServerHttpException
import java.io.IOException
import java.util.Base64
import org.bouncycastle.crypto.params.Ed25519PublicKeyParameters
import org.bouncycastle.crypto.signers.Ed25519Signer
import org.junit.Assert.assertEquals
import org.junit.Assert.assertFalse
import org.junit.Assert.assertTrue
import org.junit.Test

class DeviceRegistrarTest {
    private fun hex(s: String) = ByteArray(s.length / 2) { s.substring(it * 2, it * 2 + 2).toInt(16).toByte() }
    private val signer = Ed25519SeedSigner(hex("960d0a0436f8c133345291c51cb393064c5e47113c65f8a1b04a8bd4c38eda74"))
    private val reg = DeviceRegistration("d1", "+15555550101", listOf("+15555550102"), "ayuda", "0.1.0")

    private class FakeApi(val fail: IOException? = null) : ServerApi {
        var sent: DeviceRegistration? = null
        override fun fetchConfig(): RemoteConfig = error("unused")
        override fun registerDevice(registration: DeviceRegistration) {
            sent = registration
            fail?.let { throw it }
        }
    }

    private fun verifies(publicKey: ByteArray, message: ByteArray, signature: ByteArray): Boolean {
        val v = Ed25519Signer()
        v.init(false, Ed25519PublicKeyParameters(publicKey, 0))
        v.update(message, 0, message.size)
        return v.verifySignature(signature)
    }

    @Test fun addsPublicKeyAndSignature() {
        val api = FakeApi()
        val r = DeviceRegistrar(api, signer).register(reg)
        assertEquals(RegisterResult.Registered, r)
        val sent = api.sent!!
        assertEquals(reg, sent.copy(ed25519PublicKey = null, ed25519Signature = null))
        assertEquals("Qjj0xMgcOctYzPP1+kmbgiXDnjbvMvg+Cm1vpph2NXo=", sent.ed25519PublicKey)
    }

    @Test fun signatureVerifiesOverContextAndDeviceId() {
        val api = FakeApi()
        DeviceRegistrar(api, signer).register(reg)
        val sent = api.sent!!
        val pub = Base64.getDecoder().decode(sent.ed25519PublicKey)
        val sig = Base64.getDecoder().decode(sent.ed25519Signature)
        assertEquals(64, sig.size)
        assertTrue(verifies(pub, "MAMA-device-registration-v1d1".toByteArray(Charsets.US_ASCII), sig))
        assertFalse(verifies(pub, DeviceRegistration.signedMessage("d2"), sig))
    }

    /** Ed25519 is deterministic: the vector key over the sample's device_id gives the sample's signature. */
    @Test fun matchesTheSharedSample() {
        val api = FakeApi()
        DeviceRegistrar(api, signer).register(reg.copy(deviceId = "3f1c9a52-7b1e-4c8a-9a0e-2d6b8f4e1a77"))
        assertEquals(
            "JMio38RGA+n2az7/Xiiz4y7724rZcyDt7N90vGav0Vq8SwTXFvJSmH21VJeL323EPmedwMiyrm79YVgIjvXlAQ==",
            api.sent!!.ed25519Signature,
        )
    }

    @Test fun conflictIs409() {
        val r = DeviceRegistrar(FakeApi(ServerHttpException(409, "dup")), signer).register(reg)
        assertEquals(RegisterResult.KeyConflict, r)
    }

    @Test fun otherFailureIsFailed() {
        val r = DeviceRegistrar(FakeApi(IOException("net")), signer).register(reg)
        assertTrue(r is RegisterResult.Failed)
    }
}
