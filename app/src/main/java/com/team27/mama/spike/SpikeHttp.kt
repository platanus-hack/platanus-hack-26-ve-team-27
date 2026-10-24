package com.team27.mama.spike

import android.net.Network
import android.os.SystemClock
import java.io.IOException
import java.net.HttpURLConnection
import java.net.URL

/**
 * The spike's HTTP calls. They read and close the response instead of
 * calling disconnect(), so the pooled keep-alive connection a [warm] call
 * opened (radio up, DNS, TLS) is reused by the alert that follows.
 */
object SpikeHttp {
    private const val TIMEOUT_MS = 10_000

    /** Returns the status (-1 on a network error) and the round trip in ms. */
    fun post(
        url: String,
        body: ByteArray,
        contentType: String,
        headers: Map<String, String> = emptyMap(),
        network: Network? = null,
        onError: (String) -> Unit = {},
    ): Pair<Int, Long> = call(url, network, onError) { connection ->
        connection.requestMethod = "POST"
        connection.doOutput = true
        connection.setRequestProperty("Content-Type", contentType)
        headers.forEach { (k, v) -> connection.setRequestProperty(k, v) }
        connection.setFixedLengthStreamingMode(body.size)
        connection.outputStream.use { it.write(body) }
    }

    /** GET on the base URL (the real server answers `GET /`), only to open the connection. */
    fun warm(baseUrl: String, onError: (String) -> Unit = {}): Pair<Int, Long> =
        call(baseUrl.trim().trimEnd('/') + "/", null, onError) { it.requestMethod = "GET" }

    /** [network] pins the request to one network (mobile data next to Wi-Fi); null uses the default. */
    private fun call(url: String, network: Network?, onError: (String) -> Unit, prepare: (HttpURLConnection) -> Unit): Pair<Int, Long> {
        val start = SystemClock.elapsedRealtime()
        val status = try {
            val connection = (network?.openConnection(URL(url)) ?: URL(url).openConnection()) as HttpURLConnection
            connection.connectTimeout = TIMEOUT_MS
            connection.readTimeout = TIMEOUT_MS
            prepare(connection)
            val code = connection.responseCode
            val stream = if (code >= 400) connection.errorStream else connection.inputStream
            stream?.use { it.readBytes() }
            code
        } catch (e: IOException) {
            onError("$url failed ${e.javaClass.simpleName}: ${e.message}")
            -1
        }
        return status to SystemClock.elapsedRealtime() - start
    }
}
