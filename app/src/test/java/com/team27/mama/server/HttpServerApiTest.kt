package com.team27.mama.server

import com.team27.mama.contract.ContractJson
import com.team27.mama.contract.DeviceRegistration
import java.io.File
import java.io.IOException
import org.junit.Assert.assertEquals
import org.junit.Assert.assertNull
import org.junit.Assert.assertTrue
import org.junit.Test

class HttpServerApiTest {
    private class FakeTransport(private val response: HttpResponse) : HttpTransport {
        val calls = mutableListOf<Triple<String, String, String?>>()
        override fun execute(method: String, url: String, jsonBody: String?): HttpResponse {
            calls += Triple(method, url, jsonBody)
            return response
        }
    }

    private fun sample(): String = listOf(
        File("../server/internal/contract/testdata/remote_config.json"),
        File("server/internal/contract/testdata/remote_config.json"),
    ).first { it.isFile }.readText()

    private val registration = DeviceRegistration(
        deviceId = "d1",
        phoneNumber = "+15555550101",
        familyNumbers = listOf("+15555550102"),
        duressPhrase = "ayuda",
        ed25519PublicKey = "Qjj0xMgcOctYzPP1+kmbgiXDnjbvMvg+Cm1vpph2NXo=",
    )

    @Test fun fetchConfigGetsConfigPath() {
        val t = FakeTransport(HttpResponse(200, sample()))
        val cfg = HttpServerApi("https://example.test", t).fetchConfig()
        assertEquals("GET", t.calls.single().first)
        assertEquals("https://example.test/config", t.calls.single().second)
        assertNull(t.calls.single().third)
        assertEquals("+15555550100", cfg.smsNumber)
    }

    @Test fun registerDevicePostsJson() {
        val t = FakeTransport(HttpResponse(204, ""))
        HttpServerApi("https://example.test/", t).registerDevice(registration)
        val (m, u, body) = t.calls.single()
        assertEquals("POST", m)
        assertEquals("https://example.test/devices", u)
        assertTrue(body!!.contains("\"ed25519_public_key\""))
        assertEquals(registration, ContractJson.decodeFromString(DeviceRegistration.serializer(), body))
    }

    @Test fun non2xxThrowsServerHttpException() {
        val api = HttpServerApi("https://example.test", FakeTransport(HttpResponse(409, "x")))
        val e = runCatching { api.registerDevice(registration) }.exceptionOrNull()
        assertEquals(409, (e as ServerHttpException).code)
    }

    @Test fun badConfigBodyThrowsIOException() {
        val api = HttpServerApi("https://example.test", FakeTransport(HttpResponse(200, "{")))
        val e = runCatching { api.fetchConfig() }.exceptionOrNull()
        assertTrue(e is IOException)
    }
}
