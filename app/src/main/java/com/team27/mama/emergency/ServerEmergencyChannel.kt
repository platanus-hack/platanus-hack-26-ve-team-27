package com.team27.mama.emergency

import com.team27.mama.server.HttpTransport
import com.team27.mama.server.ServerHttpException
import java.io.IOException
import kotlinx.coroutines.CoroutineScope
import kotlinx.coroutines.Dispatchers
import kotlinx.coroutines.SupervisorJob
import kotlinx.coroutines.async

/**
 * `POST {base}/emergencies` (#57, server/internal/emergency/ingest/http.go): the body is the raw
 * SMS text, a 200 reply is the mesh ack text `MAMA1:ACK:<b64>`. Consumer: `AppGraph.emergency`.
 *
 * Non-2xx surfaces as [ServerHttpException]: the orchestrator treats 4xx as permanent and
 * retries 5xx and IO errors. [log] receives the status code of a non-2xx reply, never its body.
 *
 * The blocking call runs on [io], detached from the caller: socket connect, read and DNS ignore
 * thread interrupts, so a caller's timeout must not wait for them. Cancelling [post] returns at
 * once; the orphaned call finishes in the background and its reply is dropped.
 */
class ServerEmergencyChannel(
    baseUrl: String,
    private val transport: HttpTransport,
    private val log: (String) -> Unit = {},
    private val io: CoroutineScope = CoroutineScope(SupervisorJob() + Dispatchers.IO),
) : HttpEmergencyChannel {
    private val url = baseUrl.trimEnd('/') + "/emergencies"

    override suspend fun post(smsText: String): String {
        val res = io.async { transport.execute("POST", url, smsText) }.await()
        if (res.code !in 200..299) {
            log("POST /emergencies: HTTP ${res.code}")
            throw ServerHttpException(res.code, "POST /emergencies: HTTP ${res.code}")
        }
        val ack = res.body.trim()
        if (!ack.startsWith(ACK_PREFIX)) throw IOException("POST /emergencies: unexpected reply")
        return ack
    }

    private companion object {
        const val ACK_PREFIX = "MAMA1:ACK:"
    }
}
