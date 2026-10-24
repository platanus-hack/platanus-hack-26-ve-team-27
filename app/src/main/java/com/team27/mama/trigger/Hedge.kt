package com.team27.mama.trigger

import kotlinx.coroutines.CancellationException
import kotlinx.coroutines.CompletableDeferred
import kotlinx.coroutines.CoroutineScope
import kotlinx.coroutines.async
import kotlinx.coroutines.selects.select
import kotlinx.coroutines.withTimeoutOrNull

/** Which attempt answered and what: its HTTP status (-1 on a network error), body and round trip in ms. */
data class HedgeResult(val winner: String, val result: HttpResult) {
    val ok: Boolean get() = result.ok
}

/**
 * Runs [primary] in [scope]; if it has no 2xx after [hedgeAfterMs] (or fails
 * sooner), also runs [backup] and returns the first 2xx. When neither
 * succeeds it returns the primary's result. Both requests carry the same
 * incident_id, so the server keeps one incident. The losing request keeps
 * running in [scope] (blocking I/O) and is not waited for.
 */
suspend fun hedged(
    scope: CoroutineScope,
    hedgeAfterMs: Long,
    primary: suspend () -> HttpResult,
    backup: (suspend () -> HttpResult)?,
): HedgeResult {
    val firstOk = CompletableDeferred<HedgeResult>()
    fun attempt(name: String, call: suspend () -> HttpResult) = scope.async {
        // Any failure is a failed attempt (-1), so it can never cancel the other one.
        val result = try {
            call()
        } catch (e: CancellationException) {
            throw e
        } catch (e: Exception) {
            HttpResult.NETWORK_ERROR
        }
        HedgeResult(name, result).also { if (it.ok) firstOk.complete(it) }
    }
    val first = attempt("primary", primary)
    val early = withTimeoutOrNull(hedgeAfterMs) { first.await() }
    if (early != null && (early.ok || backup == null)) return early
    if (backup == null) return first.await()
    val second = attempt("backup", backup)
    val bothDone = scope.async { first.await(); second.await() }
    return select {
        firstOk.onAwait { it }
        bothDone.onAwait { if (firstOk.isCompleted) firstOk.getCompleted() else first.await() }
    }
}
