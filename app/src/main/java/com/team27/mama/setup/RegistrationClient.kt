package com.team27.mama.setup

import com.team27.mama.contract.ContractJson
import com.team27.mama.contract.DeviceRegistration
import kotlinx.serialization.SerializationException
import kotlinx.serialization.json.JsonObject
import kotlinx.serialization.json.JsonPrimitive
import java.io.IOException
import java.net.HttpURLConnection
import java.net.URL
import kotlinx.coroutines.Dispatchers
import kotlinx.coroutines.withContext

sealed interface RegistrationResult {
    data object Registered : RegistrationResult

    /**
     * [message] is Spanish and shown as is. [field] is set when the server
     * rejected one form field (a 422), so setup shows [message] on that field.
     */
    data class Failed(val message: String, val field: FormField? = null) : RegistrationResult
}

/** The form fields the server can reject with a 422 (docs/contract.md, DeviceRegistration). */
enum class FormField { USER_NAME, DURESS_PHRASE, SAFE_PHRASE }

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
                val status = connection.responseCode
                val answer = if (status in 200..299) "" else connection.errorStream?.use { it.readBytes().toString(Charsets.UTF_8) }.orEmpty()
                resultFor(status, answer)
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

        /** [body] is the server's answer; on a 422 its `error` names the field (it never quotes a phrase). */
        fun resultFor(status: Int, body: String = ""): RegistrationResult = when (status) {
            in 200..299 -> RegistrationResult.Registered
            422 -> rejectedField(serverError(body))
                ?: RegistrationResult.Failed("El servidor no pudo guardar tu registro (error $status). Inténtalo de nuevo.")
            else -> RegistrationResult.Failed("El servidor no pudo guardar tu registro (error $status). Inténtalo de nuevo.")
        }

        private fun rejectedField(error: String): RegistrationResult.Failed? = when {
            error.startsWith("user_name") ->
                RegistrationResult.Failed("Usa un nombre de 1 a ${SetupForm.MAX_NAME_CHARS} letras.", FormField.USER_NAME)
            error.startsWith("safe_phrase is too close") ->
                RegistrationResult.Failed(TOO_CLOSE_TO_DURESS, FormField.SAFE_PHRASE)
            error.startsWith("safe_phrase") ->
                RegistrationResult.Failed(PHRASE_LENGTH, FormField.SAFE_PHRASE)
            error.startsWith("duress_phrase") ->
                RegistrationResult.Failed(PHRASE_LENGTH, FormField.DURESS_PHRASE)
            else -> null
        }

        // The server's caps name both limits ("must be at most 150 characters and 20 words").
        private const val PHRASE_LENGTH =
            "Usa una frase más corta: de ${SetupForm.MAX_PHRASE_WORDS} palabras y ${SetupForm.MAX_PHRASE_CHARS} letras o menos."

        /** The `error` of a `{"error": "..."}` body, or "" when the body is anything else. */
        private fun serverError(body: String): String = try {
            ((ContractJson.parseToJsonElement(body) as? JsonObject)?.get("error") as? JsonPrimitive)
                ?.takeIf { it.isString }?.content.orEmpty()
        } catch (_: SerializationException) {
            ""
        }
    }
}
