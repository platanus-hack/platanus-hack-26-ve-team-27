package com.team27.mama.trigger

import com.team27.mama.contract.AlertEvent
import com.team27.mama.contract.AlertResponse
import com.team27.mama.contract.ContractJson
import kotlinx.coroutines.delay

/** How sending one AlertEvent ended. */
sealed interface AlertOutcome {
    val attempts: Int

    /** The server stored the incident. [response] is null when its 2xx body could not be read. */
    data class Accepted(val response: AlertResponse?, override val attempts: Int) : AlertOutcome

    /** The server refused this event for good (400, 409, ...): resending the same body cannot help. */
    data class Rejected(val status: Int, override val attempts: Int) : AlertOutcome

    /** No 2xx before the retries ran out. [lastStatus] is -1 for a network error. */
    data class GaveUp(val lastStatus: Int, override val attempts: Int) : AlertOutcome
}

/**
 * Sends one AlertEvent until the server takes it. Every attempt posts the
 * same body, so a retry (and the hedged copy on mobile data) carries the same
 * incident_id and the server keeps one incident: a retried incident_id
 * answers 200 with the decision it stored the first time.
 *
 * A 422 means the server does not know the device (docs/contract.md): it
 * re-sends the DeviceRegistration through [reregister], then the alert.
 * A network error, a 5xx, 408 or 429 waits the next of [retryDelaysMs] and
 * tries again; any other 4xx stops.
 */
class AlertSender(
    /** One attempt: POST /incidents with this body (the service hedges it). */
    private val post: suspend (String) -> HttpResult,
    /** POST /devices with the saved registration; true when the server stored it. */
    private val reregister: suspend () -> Boolean,
    private val retryDelaysMs: List<Long> = DEFAULT_RETRY_DELAYS_MS,
    private val sleep: suspend (Long) -> Unit = { delay(it) },
    private val onAttempt: (attempt: Int, result: HttpResult) -> Unit = { _, _ -> },
) {
    suspend fun send(event: AlertEvent): AlertOutcome {
        val body = AlertEvents.alertBody(event)
        val delays = retryDelaysMs.iterator()
        var attempts = 0
        var reregistrations = 0
        while (true) {
            attempts++
            val result = post(body)
            onAttempt(attempts, result)
            when {
                result.ok -> return AlertOutcome.Accepted(parseResponse(result.body), attempts)
                result.status == UNKNOWN_DEVICE -> {
                    // Still 422 after the server took the registration: it will not change.
                    if (reregistrations >= MAX_REREGISTRATIONS) return AlertOutcome.Rejected(result.status, attempts)
                    reregistrations++
                    if (reregister()) continue
                }
                isPermanent(result.status) -> return AlertOutcome.Rejected(result.status, attempts)
            }
            if (!delays.hasNext()) return AlertOutcome.GaveUp(result.status, attempts)
            sleep(delays.next())
        }
    }

    companion object {
        private const val UNKNOWN_DEVICE = 422
        private const val MAX_REREGISTRATIONS = 2

        /** About two minutes of retries after the first attempt. */
        val DEFAULT_RETRY_DELAYS_MS = listOf(1_000L, 2_000, 4_000, 8_000, 15_000, 15_000, 15_000, 15_000, 15_000, 15_000)

        fun isPermanent(status: Int): Boolean = status in 400..499 && status !in setOf(408, 425, 429)

        /** The AlertResponse in a 2xx body, or null when it is not one. */
        fun parseResponse(body: String): AlertResponse? =
            runCatching { ContractJson.decodeFromString(AlertResponse.serializer(), body) }.getOrNull()
    }
}
