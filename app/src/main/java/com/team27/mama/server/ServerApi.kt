package com.team27.mama.server

import com.team27.mama.contract.ContractJson
import com.team27.mama.contract.DeviceRegistration
import com.team27.mama.contract.RemoteConfig
import java.io.IOException
import kotlinx.serialization.SerializationException

data class HttpResponse(val code: Int, val body: String)

/** Blocking HTTP; throws [IOException] on network failure. */
fun interface HttpTransport {
    fun execute(method: String, url: String, jsonBody: String?): HttpResponse
}

class ServerHttpException(val code: Int, message: String) : IOException(message)

/** Blocking calls to the MAMÁ server; callers run them off the main thread. */
interface ServerApi {
    @Throws(IOException::class)
    fun fetchConfig(): RemoteConfig

    @Throws(IOException::class)
    fun registerDevice(registration: DeviceRegistration)
}

class HttpServerApi(
    baseUrl: String,
    private val transport: HttpTransport = UrlConnectionTransport(),
) : ServerApi {
    private val base = baseUrl.trimEnd('/')

    override fun fetchConfig(): RemoteConfig {
        val res = transport.execute("GET", "$base/config", null)
        check2xx("GET /config", res)
        return try {
            ContractJson.decodeFromString(RemoteConfig.serializer(), res.body)
        } catch (e: SerializationException) {
            throw IOException("GET /config: unparseable body", e)
        } catch (e: IllegalArgumentException) {
            throw IOException("GET /config: unparseable body", e)
        }
    }

    override fun registerDevice(registration: DeviceRegistration) {
        val body = ContractJson.encodeToString(DeviceRegistration.serializer(), registration)
        check2xx("POST /devices", transport.execute("POST", "$base/devices", body))
    }

    private fun check2xx(what: String, res: HttpResponse) {
        if (res.code !in 200..299) throw ServerHttpException(res.code, "$what: HTTP ${res.code}")
    }
}
