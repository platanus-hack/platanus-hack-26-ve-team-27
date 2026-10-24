package com.team27.mama.emergency

import com.team27.mama.server.HttpResponse
import com.team27.mama.server.HttpTransport
import com.team27.mama.server.ServerHttpException
import java.io.IOException
import java.util.concurrent.CountDownLatch
import kotlinx.coroutines.CoroutineScope
import kotlinx.coroutines.Dispatchers
import kotlinx.coroutines.SupervisorJob
import kotlinx.coroutines.cancel
import kotlinx.coroutines.runBlocking
import kotlinx.coroutines.test.runTest
import kotlinx.coroutines.withTimeoutOrNull
import org.junit.Assert.assertEquals
import org.junit.Assert.assertFalse
import org.junit.Assert.assertNull
import org.junit.Assert.assertTrue
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
        val logged = mutableListOf<String>()
        val channel = ServerEmergencyChannel(
            "https://example.test",
            Recording { HttpResponse(401, "secret body") },
            log = { logged += it },
        )
        val e = runCatching { channel.post("MAMA1 x") }.exceptionOrNull()
        assertEquals(401, (e as ServerHttpException).code)
        assertTrue(logged.single().contains("401"))
        assertFalse(logged.single().contains("secret body"))
    }

    /** Like a socket connect, read or DNS lookup: blocks a real thread and ignores interrupts. */
    private class Stuck : HttpTransport {
        val release = CountDownLatch(1)

        override fun execute(method: String, url: String, jsonBody: String?): HttpResponse {
            while (true) {
                try {
                    release.await()
                    return HttpResponse(200, "MAMA1:ACK:late")
                } catch (_: InterruptedException) {
                    // A blocking socket call does not wake up on an interrupt either.
                }
            }
        }
    }

    @Test
    fun timeoutReturnsWhileTheTransportIsStillBlocked() = runBlocking {
        val transport = Stuck()
        val io = CoroutineScope(SupervisorJob() + Dispatchers.IO)
        try {
            val channel = ServerEmergencyChannel("https://example.test", transport, io = io)
            val started = System.nanoTime()
            val reply = withTimeoutOrNull(100) { channel.post("MAMA1 x") }
            val elapsedMs = (System.nanoTime() - started) / 1_000_000
            assertNull(reply)
            assertTrue("post() held the caller for $elapsedMs ms", elapsedMs < 2_000)
        } finally {
            transport.release.countDown()
            io.cancel()
        }
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
