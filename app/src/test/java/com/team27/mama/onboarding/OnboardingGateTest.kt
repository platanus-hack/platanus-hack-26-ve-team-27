package com.team27.mama.onboarding

import com.team27.mama.contract.ContractJson
import com.team27.mama.contract.DeviceRegistration
import com.team27.mama.contract.RemoteConfig
import com.team27.mama.emergency.config.InMemoryConfigStore
import com.team27.mama.emergency.config.RemoteConfigRepository
import com.team27.mama.server.ServerApi
import java.io.IOException
import java.util.Base64
import java.util.concurrent.Executor
import org.junit.Assert.assertArrayEquals
import org.junit.Assert.assertEquals
import org.junit.Assert.assertNull
import org.junit.Test

class OnboardingGateTest {
    private val ed = "/x+fnERHrOfQ9HPdLiQP9ya2eBtqDNBmsrjsP9ZnMBE="
    private val x = "14/r5bky56/VN9vYfVVwD79P59iaXXdMRrOvYZxUGj0="
    private val sample = RemoteConfig("+15555550100", ed, x)
    private val inline = Executor { it.run() }

    private class CountingApi(val reply: () -> RemoteConfig) : ServerApi {
        var calls = 0
        override fun fetchConfig(): RemoteConfig {
            calls++
            return reply()
        }
        override fun registerDevice(registration: DeviceRegistration) = Unit
    }

    /** Holds background work until [runAll], to show [OnboardingGate.finish] does not wait for it. */
    private class QueuedExecutor : Executor {
        val queued = mutableListOf<Runnable>()
        override fun execute(command: Runnable) {
            queued += command
        }
        fun runAll() = queued.toList().also { queued.clear() }.forEach { it.run() }
    }

    private fun cachedStore() = InMemoryConfigStore(ContractJson.encodeToString(RemoteConfig.serializer(), sample))

    private fun gate(
        online: Boolean,
        api: CountingApi,
        store: InMemoryConfigStore = InMemoryConfigStore(),
        background: Executor = inline,
    ) = OnboardingGate({ online }, RemoteConfigRepository(api, store, "+15555550199"), background)

    @Test fun offlineDoesNotCallServer() {
        val api = CountingApi { sample }
        assertEquals(OnboardingResult.Offline, gate(false, api).finish())
        assertEquals(0, api.calls)
    }

    @Test fun noCacheOfflineShowsOfflineMessage() {
        val result = gate(false, CountingApi { sample }).finish()
        assertEquals(OnboardingResult.Offline, result)
        assertEquals(
            "Necesitas internet para terminar de configurar Aló Mamá. Conéctate a wifi o datos y vuelve a intentarlo.",
            result.userMessage(),
        )
    }

    @Test fun cachedKeysOfflineIsDone() {
        val api = CountingApi { sample }
        assertEquals(OnboardingResult.Done, gate(false, api, cachedStore()).finish())
        assertEquals(0, api.calls)
    }

    @Test fun cachedKeysOnlineIsDoneAndRefreshesInBackground() {
        val api = CountingApi { sample }
        val background = QueuedExecutor()
        assertEquals(OnboardingResult.Done, gate(true, api, cachedStore(), background).finish())
        assertEquals(0, api.calls)
        background.runAll()
        assertEquals(1, api.calls)
    }

    @Test fun cachedKeysServerDownIsStillDone() {
        val api = CountingApi { throw IOException("down") }
        val store = cachedStore()
        assertEquals(OnboardingResult.Done, gate(true, api, store).finish())
        assertEquals(1, api.calls)
        assertEquals(ContractJson.encodeToString(RemoteConfig.serializer(), sample), store.json)
    }

    @Test fun onlineRefreshesConfig() {
        val api = CountingApi { sample }
        val store = InMemoryConfigStore()
        val repo = RemoteConfigRepository(api, store, "+15555550199")
        assertEquals(OnboardingResult.Done, OnboardingGate({ true }, repo, inline).finish())
        assertArrayEquals(Base64.getDecoder().decode(ed), repo.current().serverEd25519PublicKey)
        assertArrayEquals(Base64.getDecoder().decode(x), repo.current().serverX25519PublicKey)
    }

    @Test fun serverDownIsServerUnavailable() {
        val api = CountingApi { throw IOException("down") }
        assertEquals(OnboardingResult.ServerUnavailable, gate(true, api).finish())
    }

    @Test fun messages() {
        assertNull(OnboardingResult.Done.userMessage())
        assertEquals(
            "Necesitas internet para terminar de configurar Aló Mamá. Conéctate a wifi o datos y vuelve a intentarlo.",
            OnboardingResult.Offline.userMessage(),
        )
        assertEquals(
            "No pudimos conectar con Aló Mamá. Revisa tu conexión y vuelve a intentarlo.",
            OnboardingResult.ServerUnavailable.userMessage(),
        )
    }
}
