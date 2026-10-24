package com.team27.mama.emergency

import com.team27.mama.server.HttpResponse
import com.team27.mama.server.HttpTransport
import com.team27.mama.server.ServerHttpException
import java.io.IOException
import kotlinx.coroutines.test.runTest
import org.junit.Assert.assertEquals
import org.junit.Assert.assertThrows
import org.junit.Test

class ServerEmergencyChannelTest {
    private class Recording(private val reply: () -> HttpResponse) : HttpTransport {
        val calls = mutableListOf<Triple<String, String, String?>>()
        override fun execute(method: String, url: String, jsonBody: String?): HttpResponse {
            calls += Triple(method, url, jsonBody)
            return reply()
        }
    }

    @Test
    fun postsToEmergencies() = runTest {
        val transport = Recording { HttpResponse(200, "MAMA1:ACK:abc\n") }
        val ack = ServerEmergencyChannel("https://example.test/", transport).post("MAMA1 xyz")
        assertEquals("MAMA1:ACK:abc", ack)
        assertEquals(listOf(Triple("POST", "https://example.test/emergencies", "MAMA1 xyz")), transport.calls)
    }

    @Test
    fun non2xxThrows() = runTest {
        val channel = ServerEmergencyChannel("https://example.test", Recording { HttpResponse(401, "no") })
        val e = runCatching { channel.post("MAMA1 x") }.exceptionOrNull()
        assertEquals(401, (e as ServerHttpException).code)
    }

    @Test
    fun badReplyThrowsIOException() = runTest {
        for (body in listOf("{", "")) {
            val channel = ServerEmergencyChannel("https://example.test", Recording { HttpResponse(200, body) })
            val e = runCatching { channel.post("MAMA1 x") }.exceptionOrNull()
            assertEquals(IOException::class.java, e?.javaClass)
        }
    }

    @Test
    fun transportIOExceptionPropagates() = runTest {
        val channel = ServerEmergencyChannel("https://example.test", Recording { throw IOException("down") })
        val e = runCatching { channel.post("MAMA1 x") }.exceptionOrNull()
        assertEquals("down", e?.message)
    }
}
