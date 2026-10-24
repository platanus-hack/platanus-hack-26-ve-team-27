package com.team27.mama.relay

import com.team27.mama.emergency.payload.MamaText
import com.team27.mama.server.HttpResponse
import com.team27.mama.server.HttpTransport
import java.io.IOException
import kotlinx.coroutines.test.runTest
import org.junit.Assert.assertEquals
import org.junit.Test

class ServerRelayUploaderTest {
    private class FakeTransport(var reply: () -> HttpResponse) : HttpTransport {
        val calls = mutableListOf<Triple<String, String, String?>>()

        override fun execute(method: String, url: String, jsonBody: String?): HttpResponse {
            calls += Triple(method, url, jsonBody)
            return reply()
        }
    }

    private fun uploader(t: FakeTransport) = ServerRelayUploader("https://example.test/", t)

    @Test
    fun postsTextAndHops() = runTest {
        val t = FakeTransport { HttpResponse(200, ACK + "\n") }
        val r = uploader(t).upload(seal(1), 2)
        assertEquals(UploadResult.Acked(ACK), r)
        assertEquals(
            listOf(Triple("POST", "https://example.test/emergencies/relay", "{\"text\":\"${seal(1)}\",\"hops\":2}")),
            t.calls,
        )
    }

    @Test
    fun fourXxIsRejected() = runTest {
        for (code in listOf(400, 403, 409, 413, 422)) {
            assertEquals(UploadResult.Rejected, uploader(FakeTransport { HttpResponse(code, "{}") }).upload(seal(1), 0))
        }
    }

    @Test
    fun fiveXxAndNetworkAreFailed() = runTest {
        for (code in listOf(500, 503)) {
            assertEquals(UploadResult.Failed, uploader(FakeTransport { HttpResponse(code, "") }).upload(seal(1), 0))
        }
        assertEquals(UploadResult.Failed, uploader(FakeTransport { throw IOException("down") }).upload(seal(1), 0))
    }

    @Test
    fun unexpectedExceptionIsFailed() = runTest {
        val t = FakeTransport { throw IllegalStateException("x") }
        assertEquals(UploadResult.Failed, uploader(t).upload(seal(1), 0))
    }

    @Test
    fun okWithoutAckIsFailed() = runTest {
        for (body in listOf("<html>", seal(1), MamaText.meshAck(ByteArray(75)))) {
            assertEquals(UploadResult.Failed, uploader(FakeTransport { HttpResponse(200, body) }).upload(seal(1), 0))
        }
    }
}
