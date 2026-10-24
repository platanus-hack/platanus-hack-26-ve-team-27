package com.team27.mama.emergency

import com.team27.mama.server.HttpTransport
import com.team27.mama.server.ServerHttpException
import java.io.IOException
import kotlinx.coroutines.Dispatchers
import kotlinx.coroutines.runInterruptible

/**
 * `POST {base}/emergencies` (#57, server/internal/emergency/ingest/http.go): the body is the raw
 * SMS text, a 200 reply is the mesh ack text `MAMA1:ACK:<b64>`. Consumer: `AppGraph.emergency`.
 *
 * Non-2xx surfaces as [ServerHttpException]: the orchestrator treats 4xx as permanent and
 * retries 5xx and IO errors.
 */
class ServerEmergencyChannel(
    baseUrl: String,
    private val transport: HttpTransport,
) : HttpEmergencyChannel {
    private val url = baseUrl.trimEnd('/') + "/emergencies"

    override suspend fun post(smsText: String): String {
        val res = runInterruptible(Dispatchers.IO) { transport.execute("POST", url, smsText) }
        if (res.code !in 200..299) throw ServerHttpException(res.code, "POST /emergencies: HTTP ${res.code}")
        val ack = res.body.trim()
        if (!ack.startsWith(ACK_PREFIX)) throw IOException("POST /emergencies: unexpected reply")
        return ack
    }

    private companion object {
        const val ACK_PREFIX = "MAMA1:ACK:"
    }
}
