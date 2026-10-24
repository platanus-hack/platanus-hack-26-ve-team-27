package com.team27.mama.onboarding

import com.team27.mama.contract.DeviceRegistration
import com.team27.mama.contract.RemoteConfig
import com.team27.mama.emergency.config.InMemoryConfigStore
import com.team27.mama.emergency.config.RemoteConfigRepository
import com.team27.mama.server.ServerApi
import java.io.IOException
import java.util.Base64
import org.junit.Assert.assertArrayEquals
import org.junit.Assert.assertEquals
import org.junit.Assert.assertNull
import org.junit.Test

class OnboardingGateTest {
    private val ed = "/x+fnERHrOfQ9HPdLiQP9ya2eBtqDNBmsrjsP9ZnMBE="
    private val x = "14/r5bky56/VN9vYfVVwD79P59iaXXdMRrOvYZxUGj0="
    private val sample = RemoteConfig("+15555550100", ed, x)

    private class CountingApi(val reply: () -> RemoteConfig) : ServerApi {
        var calls = 0
        override fun fetchConfig(): RemoteConfig {
            calls++
            return reply()
        }
        override fun registerDevice(registration: DeviceRegistration) = Unit
    }

    private fun gate(online: Boolean, api: CountingApi, store: InMemoryConfigStore = InMemoryConfigStore()) =
        OnboardingGate({ online }, RemoteConfigRepository(api, store, "+15555550199")) to store

    @Test fun offlineDoesNotCallServer() {
        val api = CountingApi { sample }
        assertEquals(OnboardingResult.Offline, gate(false, api).first.finish())
        assertEquals(0, api.calls)
    }

    @Test fun onlineRefreshesConfig() {
        val api = CountingApi { sample }
        val store = InMemoryConfigStore()
        val repo = RemoteConfigRepository(api, store, "+15555550199")
        assertEquals(OnboardingResult.Done, OnboardingGate({ true }, repo).finish())
        assertArrayEquals(Base64.getDecoder().decode(ed), repo.current().serverEd25519PublicKey)
        assertArrayEquals(Base64.getDecoder().decode(x), repo.current().serverX25519PublicKey)
    }

    @Test fun serverDownIsServerUnavailable() {
        val api = CountingApi { throw IOException("down") }
        assertEquals(OnboardingResult.ServerUnavailable, gate(true, api).first.finish())
    }

    @Test fun messages() {
        assertNull(OnboardingResult.Done.userMessage())
        assertEquals(
            "Necesitas internet para terminar de configurar MAMÁ. Conéctate a wifi o datos y vuelve a intentarlo.",
            OnboardingResult.Offline.userMessage(),
        )
        assertEquals(
            "No pudimos conectar con MAMÁ. Revisa tu conexión y vuelve a intentarlo.",
            OnboardingResult.ServerUnavailable.userMessage(),
        )
    }
}
