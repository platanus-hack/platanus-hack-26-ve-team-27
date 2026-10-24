package com.team27.mama.onboarding

import com.team27.mama.contract.DeviceRegistration
import com.team27.mama.contract.RemoteConfig
import com.team27.mama.emergency.payload.Ed25519SeedSigner
import com.team27.mama.server.ServerApi
import com.team27.mama.server.ServerHttpException
import java.io.IOException
import org.junit.Assert.assertEquals
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

    @Test fun addsPublicKey() {
        val api = FakeApi()
        val r = DeviceRegistrar(api, signer).register(reg)
        assertEquals(RegisterResult.Registered, r)
        assertEquals(reg.copy(ed25519PublicKey = "Qjj0xMgcOctYzPP1+kmbgiXDnjbvMvg+Cm1vpph2NXo="), api.sent)
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
