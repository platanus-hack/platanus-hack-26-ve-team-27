package com.team27.mama.emergency.config

import com.team27.mama.contract.DeviceRegistration
import com.team27.mama.contract.RemoteConfig
import com.team27.mama.server.ServerApi
import com.team27.mama.server.ServerHttpException
import java.io.IOException
import java.util.Base64
import org.junit.Assert.assertArrayEquals
import org.junit.Assert.assertEquals
import org.junit.Assert.assertFalse
import org.junit.Assert.assertNotNull
import org.junit.Assert.assertNull
import org.junit.Assert.assertTrue
import org.junit.Test

class RemoteConfigRepositoryTest {
    private val default = "+15555550199"
    private val ed = "/x+fnERHrOfQ9HPdLiQP9ya2eBtqDNBmsrjsP9ZnMBE="
    private val x = "14/r5bky56/VN9vYfVVwD79P59iaXXdMRrOvYZxUGj0="
    private val sample = RemoteConfig("+15555550100", ed, x)

    private class FakeApi(var reply: () -> RemoteConfig) : ServerApi {
        override fun fetchConfig(): RemoteConfig = reply()
        override fun registerDevice(registration: DeviceRegistration) = Unit
    }

    @Test fun currentWithoutCacheUsesDefault() {
        val c = RemoteConfigRepository(FakeApi { sample }, InMemoryConfigStore(), default).current()
        assertEquals(default, c.smsNumber)
        assertNull(c.serverEd25519PublicKey)
        assertNull(c.serverX25519PublicKey)
    }

    @Test fun refreshCachesAndServes() {
        val store = InMemoryConfigStore()
        val repo = RemoteConfigRepository(FakeApi { sample }, store, default)
        assertTrue(repo.refresh())
        assertNotNull(store.json)
        val offline = RemoteConfigRepository(FakeApi { throw IOException("offline") }, store, default).current()
        assertEquals("+15555550100", offline.smsNumber)
        assertArrayEquals(Base64.getDecoder().decode(ed), offline.serverEd25519PublicKey)
        assertArrayEquals(Base64.getDecoder().decode(x), offline.serverX25519PublicKey)
        assertEquals(offline.smsNumber, repo.current().smsNumber)
    }

    @Test fun missingNumberFallsBackToDefault() {
        val repo = RemoteConfigRepository(FakeApi { sample.copy(smsNumber = null) }, InMemoryConfigStore(), default)
        assertTrue(repo.refresh())
        val c = repo.current()
        assertEquals(default, c.smsNumber)
        assertNotNull(c.serverEd25519PublicKey)
    }

    @Test fun refreshKeepsCacheOnBadReply() {
        val store = InMemoryConfigStore()
        val api = FakeApi { sample }
        val repo = RemoteConfigRepository(api, store, default)
        assertTrue(repo.refresh())
        val before = store.json
        val bad31 = Base64.getEncoder().encodeToString(ByteArray(31))
        api.reply = { sample.copy(smsNumber = "+15555550177", serverX25519PublicKey = bad31) }
        assertFalse(repo.refresh())
        assertEquals(before, store.json)
        assertEquals("+15555550100", repo.current().smsNumber)
        api.reply = { throw ServerHttpException(500, "boom") }
        assertFalse(repo.refresh())
        assertEquals(before, store.json)
        assertEquals("+15555550100", repo.current().smsNumber)
    }

    @Test fun currentFallsBackToDefaultOnCorruptCache() {
        val repo = RemoteConfigRepository(FakeApi { sample }, InMemoryConfigStore("{\"sms_number\":"), default)
        val c = repo.current()
        assertEquals(default, c.smsNumber)
        assertNull(c.serverEd25519PublicKey)
    }
}
