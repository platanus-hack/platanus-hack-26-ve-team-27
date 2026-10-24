package com.team27.mama.relay

import com.team27.mama.emergency.payload.MamaText
import com.team27.mama.emergency.payload.MeshFrame
import com.team27.mama.server.HttpTransport
import kotlinx.coroutines.CancellationException
import kotlinx.coroutines.Dispatchers
import kotlinx.coroutines.runInterruptible
import kotlinx.serialization.Serializable
import kotlinx.serialization.json.Json

/** Kotlin side of #57's `ingest.RelayUpload`: the body of `POST /emergencies/relay`. */
@Serializable
data class RelayUpload(val text: String, val hops: Int)

/**
 * `POST {base}/emergencies/relay` (#57). 200 with a valid 76-byte ack is [UploadResult.Acked], any 4xx but
 * 408 and 429 is permanent ([UploadResult.Rejected]), everything else is [UploadResult.Failed]. Only the caller's own
 * cancellation escapes. Nothing is logged: a text never leaves this class except in the request.
 *
 * The transport's own timeouts bound the call, so there is no `withTimeout` here.
 */
class ServerRelayUploader(
    baseUrl: String,
    private val transport: HttpTransport,
) : RelayUploader {
    private val url = baseUrl.trimEnd('/') + "/emergencies/relay"

    override suspend fun upload(text: String, hops: Int): UploadResult {
        val body = Json.encodeToString(RelayUpload.serializer(), RelayUpload(text, hops))
        return try {
            val res = runInterruptible(Dispatchers.IO) { transport.execute("POST", url, body) }
            when (res.code) {
                200 -> ackOrFailed(res.body)
                // #57 never sends these: they come from the proxy/CDN infrastructure in front of it
                // (a timeout or throttling), so the text was not judged and is retried.
                408, 429 -> UploadResult.Failed
                in 400..499 -> UploadResult.Rejected
                else -> UploadResult.Failed
            }
        } catch (e: CancellationException) {
            throw e
        } catch (e: Exception) {
            UploadResult.Failed
        }
    }

    private fun ackOrFailed(body: String): UploadResult {
        // Trimmed on purpose: the returned ack is the trimmed text, never the raw body.
        val ack = body.trim()
        val frame = try {
            MamaText.parseMesh(ack)
        } catch (e: Exception) {
            return UploadResult.Failed
        }
        return if (frame is MeshFrame.Ack && frame.ack.size == RelayLimits.ACK_BYTES) {
            UploadResult.Acked(ack)
        } else {
            UploadResult.Failed
        }
    }
}
