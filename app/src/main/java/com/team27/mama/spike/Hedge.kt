package com.team27.mama.spike

import kotlinx.coroutines.CancellationException
import kotlinx.coroutines.CompletableDeferred
import kotlinx.coroutines.CoroutineScope
import kotlinx.coroutines.async
import kotlinx.coroutines.selects.select
import kotlinx.coroutines.withTimeoutOrNull

/** Which attempt answered, its HTTP status (-1 on a network error) and its round trip in ms. */
data class HedgeResult(val winner: String, val status: Int, val ms: Long) {
    val ok: Boolean get() = status in 200..299
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
    primary: suspend () -> Pair<Int, Long>,
    backup: (suspend () -> Pair<Int, Long>)?,
): HedgeResult {
    val firstOk = CompletableDeferred<HedgeResult>()
    fun attempt(name: String, call: suspend () -> Pair<Int, Long>) = scope.async {
        // Any failure is a failed attempt (-1), so it can never cancel the other one.
        val (status, ms) = try {
            call()
        } catch (e: CancellationException) {
            throw e
        } catch (e: Exception) {
            -1 to 0L
        }
        HedgeResult(name, status, ms).also { if (it.ok) firstOk.complete(it) }
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
