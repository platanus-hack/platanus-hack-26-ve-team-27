package com.team27.mama.setup

import com.team27.mama.contract.ContractJson
import com.team27.mama.contract.DeviceRegistration
import java.io.IOException
import java.net.HttpURLConnection
import java.net.URL
import kotlinx.coroutines.Dispatchers
import kotlinx.coroutines.withContext

sealed interface RegistrationResult {
    data object Registered : RegistrationResult

    /** [message] is Spanish and shown as is. */
    data class Failed(val message: String) : RegistrationResult
}

/** `POST /devices` (docs/contract.md, DeviceRegistration). The server answers 204 and upserts the row. */
class RegistrationClient(baseUrl: String) {
    private val devicesUrl = devicesUrl(baseUrl)

    suspend fun register(registration: DeviceRegistration): RegistrationResult = withContext(Dispatchers.IO) {
        val body = registrationBody(registration).toByteArray(Charsets.UTF_8)
        try {
            val connection = URL(devicesUrl).openConnection() as HttpURLConnection
            try {
                connection.requestMethod = "POST"
                connection.connectTimeout = TIMEOUT_MS
                connection.readTimeout = TIMEOUT_MS
                connection.doOutput = true
                connection.setRequestProperty("Content-Type", "application/json; charset=utf-8")
                connection.setFixedLengthStreamingMode(body.size)
                connection.outputStream.use { it.write(body) }
                resultFor(connection.responseCode)
            } finally {
                connection.disconnect()
            }
        } catch (_: IOException) {
            RegistrationResult.Failed(NETWORK_ERROR)
        }
    }

    companion object {
        private const val TIMEOUT_MS = 15_000

        /** Shown when the server cannot be reached. */
        const val NETWORK_ERROR = "No pudimos conectar con el servidor. Revisa tu conexión e inténtalo de nuevo."

        fun devicesUrl(baseUrl: String): String = baseUrl.trim().trimEnd('/') + "/devices"

        fun registrationBody(registration: DeviceRegistration): String =
            ContractJson.encodeToString(DeviceRegistration.serializer(), registration)

        fun resultFor(status: Int): RegistrationResult = when (status) {
            in 200..299 -> RegistrationResult.Registered
            else -> RegistrationResult.Failed("El servidor no pudo guardar tu registro (error $status). Inténtalo de nuevo.")
        }
    }
}
