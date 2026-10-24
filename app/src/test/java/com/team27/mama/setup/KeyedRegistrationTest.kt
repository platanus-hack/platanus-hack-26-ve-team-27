package com.team27.mama.setup

import com.team27.mama.contract.DeviceRegistration
import com.team27.mama.contract.RemoteConfig
import com.team27.mama.emergency.EmergencyTestKit
import com.team27.mama.emergency.FlakySigner
import com.team27.mama.emergency.payload.PayloadSigner
import com.team27.mama.onboarding.DeviceRegistrar
import com.team27.mama.server.ServerApi
import com.team27.mama.server.ServerHttpException
import java.io.IOException
import kotlinx.coroutines.ExperimentalCoroutinesApi
import kotlinx.coroutines.test.StandardTestDispatcher
import kotlinx.coroutines.test.runTest
import org.junit.Assert.assertEquals
import org.junit.Assert.assertNotNull
import org.junit.Test

@OptIn(ExperimentalCoroutinesApi::class)
class KeyedRegistrationTest {
    private val reg = DeviceRegistration("d1", "+15555550101", listOf("+15555550102"), "ayuda", "0.1.0")

    private class FakeApi(val fail: IOException? = null) : ServerApi {
        var sent: DeviceRegistration? = null
        override fun fetchConfig(): RemoteConfig = error("unused")
        override fun registerDevice(registration: DeviceRegistration) {
            sent = registration
            fail?.let { throw it }
        }
    }

    /** [KeyedRegistration.register] on the test scheduler instead of Dispatchers.IO. */
    private fun run(api: ServerApi, signer: PayloadSigner = EmergencyTestKit.DEVICE): RegistrationResult {
        var result: RegistrationResult? = null
        runTest { result = KeyedRegistration.register(DeviceRegistrar(api, signer), reg, StandardTestDispatcher(testScheduler)) }
        return result!!
    }

    @Test
    fun registeredSendsTheDeviceKey() {
        val api = FakeApi()
        assertEquals(RegistrationResult.Registered, run(api))
        assertNotNull(api.sent?.ed25519PublicKey)
        assertNotNull(api.sent?.ed25519Signature)
        assertEquals(reg.deviceId, api.sent?.deviceId)
    }

    @Test
    fun serverErrorsUseRegistrationClientsMessage() {
        assertEquals(RegistrationClient.resultFor(500), run(FakeApi(ServerHttpException(500, "POST /devices: HTTP 500"))))
        assertEquals(RegistrationClient.resultFor(409), run(FakeApi(ServerHttpException(409, "POST /devices: HTTP 409"))))
        assertEquals(RegistrationClient.resultFor(400), run(FakeApi(ServerHttpException(400, "POST /devices: HTTP 400"))))
    }

    @Test
    fun networkErrorUsesRegistrationClientsMessage() {
        assertEquals(RegistrationResult.Failed(RegistrationClient.NETWORK_ERROR), run(FakeApi(IOException("timeout"))))
    }

    @Test
    fun unusableKeystoreAsksToTryAgain() {
        assertEquals(
            RegistrationResult.Failed("No pudimos preparar la llave de este teléfono. Inténtalo de nuevo."),
            run(FakeApi(), FlakySigner(broken = true)),
        )
    }
}
