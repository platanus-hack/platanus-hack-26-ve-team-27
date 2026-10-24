package com.team27.mama.trigger

import android.net.Network
import android.os.SystemClock
import java.io.IOException
import java.net.HttpURLConnection
import java.net.URL

/** One HTTP answer: the status (-1 on a network error), the body and the round trip in ms. */
data class HttpResult(val status: Int, val body: String = "", val ms: Long = 0) {
    val ok: Boolean get() = status in 200..299

    companion object {
        val NETWORK_ERROR = HttpResult(-1)
    }
}

/**
 * The trigger's HTTP calls. They read and close the response instead of
 * calling disconnect(), so the pooled keep-alive connection a [warm] call
 * opened (radio up, DNS, TLS) is reused by the alert that follows.
 */
object TriggerHttp {
    private const val TIMEOUT_MS = 10_000
    private const val JSON = "application/json; charset=utf-8"

    /** POSTs a JSON body. [network] pins the request to one network (mobile data next to Wi-Fi); null uses the default. */
    fun postJson(url: String, body: String, network: Network? = null, onError: (String) -> Unit = {}): HttpResult {
        val bytes = body.toByteArray(Charsets.UTF_8)
        return call(url, network, onError) { connection ->
            connection.requestMethod = "POST"
            connection.doOutput = true
            connection.setRequestProperty("Content-Type", JSON)
            connection.setFixedLengthStreamingMode(bytes.size)
            connection.outputStream.use { it.write(bytes) }
        }
    }

    /** GET on the base URL (the real server answers `GET /`), only to open the connection. */
    fun warm(baseUrl: String, onError: (String) -> Unit = {}): HttpResult =
        call(AlertEvents.rootUrl(baseUrl), null, onError) { it.requestMethod = "GET" }

    private fun call(url: String, network: Network?, onError: (String) -> Unit, prepare: (HttpURLConnection) -> Unit): HttpResult {
        val start = SystemClock.elapsedRealtime()
        return try {
            val connection = (network?.openConnection(URL(url)) ?: URL(url).openConnection()) as HttpURLConnection
            connection.connectTimeout = TIMEOUT_MS
            connection.readTimeout = TIMEOUT_MS
            prepare(connection)
            val code = connection.responseCode
            val stream = if (code >= 400) connection.errorStream else connection.inputStream
            val body = stream?.use { it.readBytes().toString(Charsets.UTF_8) }.orEmpty()
            HttpResult(code, body, SystemClock.elapsedRealtime() - start)
        } catch (e: IOException) {
            onError("$url failed ${e.javaClass.simpleName}: ${e.message}")
            HttpResult(-1, ms = SystemClock.elapsedRealtime() - start)
        }
    }
}
