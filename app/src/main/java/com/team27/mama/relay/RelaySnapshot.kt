package com.team27.mama.relay

import java.security.MessageDigest
import kotlinx.serialization.Serializable
import kotlinx.serialization.json.Json

object RelayTiming {
    /** Retry gaps double from the initial gap up to the cap: tries at t = 0, 5, 15, 35, 75, 155, 315, 615, 915 … s. */
    const val RETRY_INITIAL_MS = 5_000L
    const val RETRY_MAX_MS = 300_000L

    /** While offline with uploads pending, `isOnline()` is re-checked this often even without a network callback. */
    const val OFFLINE_POLL_MS = 60_000L

    /** A pending upload or a seen entry expires this long after it was heard (bitchat's `MESSAGE_CACHE_TIMEOUT_MS`). */
    const val ENTRY_TTL_MS = 43_200_000L

    /** A stored ack goes out again at most this often when the emitter repeats its text. */
    const val ACK_REBROADCAST_MIN_INTERVAL_MS = 10_000L

    /** The gap after the [failures]-th failed upload (1-based): `min(5 000 × 2^(n-1), 300 000)` ms. */
    fun retryDelayMs(failures: Int): Long {
        var gap = RETRY_INITIAL_MS
        repeat((failures - 1).coerceAtLeast(0)) {
            if (gap >= RETRY_MAX_MS) return RETRY_MAX_MS
            gap *= 2
        }
        return gap.coerceAtMost(RETRY_MAX_MS)
    }
}

object RelayLimits {
    /** bitchat's `MAX_CACHED_MESSAGES`; when full, the oldest heard is dropped. */
    const val MAX_PENDING = 100
    const val MAX_SEEN = 500
    const val MAX_OWN = 64
    const val MAX_TEXT_CHARS = 512
    const val SEALED_BOX_BYTES = 146
    const val ACK_BYTES = 76
}

/** Everything the relay persists, as one JSON document (key `relay_state_json`). */
@Serializable
internal data class RelaySnapshot(
    val v: Int = 1,
    /** Oldest heard first. */
    val pending: List<PendingUpload> = emptyList(),
    /** Oldest first seen first. */
    val seen: List<SeenText> = emptyList(),
    val relayedCount: Int = 0,
)

/** A heard SEAL text not yet delivered; [attempts] counts failed uploads. */
@Serializable
internal data class PendingUpload(
    val text: String,
    val hops: Int,
    val heardAtMs: Long,
    val attempts: Int = 0,
    val nextAttemptAtMs: Long,
)

/** A SEAL text already heard, by [relayHash]; [ackText] is the server ack once delivered. */
@Serializable
internal data class SeenText(
    val hash: String,
    val firstSeenAtMs: Long,
    val ackText: String? = null,
    val lastAckBroadcastAtMs: Long = 0L,
)

internal val RelayJson = Json { ignoreUnknownKeys = true }

/** The first 32 lowercase hex chars of SHA-256 over [text] in UTF-8. */
internal fun relayHash(text: String): String {
    val digest = MessageDigest.getInstance("SHA-256").digest(text.toByteArray(Charsets.UTF_8))
    return digest.take(16).joinToString("") { "%02x".format(it.toInt() and 0xff) }
}
