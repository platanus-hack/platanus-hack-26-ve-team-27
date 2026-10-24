package com.team27.mama.server

import java.net.HttpURLConnection
import java.net.URL

/** Thin HttpURLConnection adapter; not unit tested (network). */
class UrlConnectionTransport : HttpTransport {
    override fun execute(method: String, url: String, jsonBody: String?): HttpResponse {
        val conn = URL(url).openConnection() as HttpURLConnection
        try {
            conn.requestMethod = method
            conn.connectTimeout = TIMEOUT_MS
            conn.readTimeout = TIMEOUT_MS
            if (jsonBody != null) {
                conn.doOutput = true
                conn.setRequestProperty("Content-Type", "application/json; charset=utf-8")
                conn.outputStream.use { it.write(jsonBody.toByteArray(Charsets.UTF_8)) }
            }
            val code = conn.responseCode
            val stream = if (code in 200..299) conn.inputStream else conn.errorStream
            val body = stream?.use { it.readBytes().toString(Charsets.UTF_8) }.orEmpty()
            return HttpResponse(code, body)
        } finally {
            conn.disconnect()
        }
    }

    private companion object {
        const val TIMEOUT_MS = 10_000
    }
}
