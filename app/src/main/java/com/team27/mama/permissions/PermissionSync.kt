package com.team27.mama.permissions

import com.team27.mama.contract.ContractJson
import com.team27.mama.contract.PermissionEvent
import com.team27.mama.contract.PermissionEvents
import com.team27.mama.server.HttpTransport
import com.team27.mama.server.UrlConnectionTransport
import java.io.IOException
import java.util.concurrent.Executor

/**
 * Capa 2 of #125 wired up: [sync] notes the permissions as the system
 * reports them now ([PermissionLog.observe]) and, once [ready] (setup done:
 * the server knows the device), sends what the server has not confirmed.
 * Setup syncs on every permission change and when it finishes; the app on
 * every resume; the trigger after each alert. All of it runs on [executor],
 * one at a time, so callers never wait and never race.
 */
class PermissionSync(
    private val log: PermissionLog,
    private val read: () -> Map<String, Boolean>,
    /** POSTs a batch; returns the HTTP status, -1 when the server could not be reached. */
    private val send: (List<PermissionEvent>) -> Int,
    private val ready: () -> Boolean,
    private val executor: Executor,
    private val now: () -> Long = System::currentTimeMillis,
    private val onError: (String) -> Unit = {},
) {
    /** Returns at once; the work runs on [executor]. */
    fun sync() = executor.execute {
        try {
            log.observe(read(), now())
            if (ready()) flush()
        } catch (e: Exception) {
            // A broken read or store must never crash the app or the trigger.
            onError("permission sync failed ${e.javaClass.simpleName}")
        }
    }

    /**
     * Sends [PermissionLog.pending] in batches of [MAX_PER_REQUEST] and drops
     * what the server took. Stops at the first batch that failed, to retry
     * next time; a batch the server refused as malformed (400) is dropped,
     * since sending it again would never work. A 422 (device not registered)
     * keeps everything. Blocking. True when nothing is left to send.
     */
    fun flush(): Boolean {
        for (batch in log.pending().chunked(MAX_PER_REQUEST)) {
            when (val status = send(batch)) {
                in 200..299, 400 -> log.confirm(batch)
                else -> {
                    onError("permission events not sent: HTTP $status")
                    return false
                }
            }
        }
        return log.pending().isEmpty()
    }

    companion object {
        /** The server's cap per request (intake.maxPermissionEvents). */
        const val MAX_PER_REQUEST = 50
    }
}

/** `POST /devices/{device_id}/permissions` (docs/contract.md, PermissionEvents). Blocking. */
class PermissionEventsClient(baseUrl: String, private val transport: HttpTransport = UrlConnectionTransport()) {
    private val base = baseUrl.trim().trimEnd('/')

    /** The HTTP status, or -1 when the server could not be reached. */
    fun send(deviceId: String, events: List<PermissionEvent>): Int = try {
        transport.execute("POST", url(base, deviceId), body(events)).code
    } catch (_: IOException) {
        -1
    }

    companion object {
        fun url(baseUrl: String, deviceId: String) = baseUrl.trim().trimEnd('/') + "/devices/$deviceId/permissions"

        fun body(events: List<PermissionEvent>): String =
            ContractJson.encodeToString(PermissionEvents.serializer(), PermissionEvents(events))
    }
}
