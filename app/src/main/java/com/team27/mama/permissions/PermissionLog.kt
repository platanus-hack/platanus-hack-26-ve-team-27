package com.team27.mama.permissions

import com.team27.mama.contract.ContractJson
import com.team27.mama.contract.PermissionEvent
import com.team27.mama.contract.PermissionKeys
import com.team27.mama.setup.KeyValueStore
import java.time.Instant
import kotlinx.serialization.KSerializer
import kotlinx.serialization.SerializationException
import kotlinx.serialization.builtins.ListSerializer
import kotlinx.serialization.builtins.MapSerializer
import kotlinx.serialization.builtins.serializer

/**
 * Capa 2 of #125: when each permission was granted or lost, as this phone saw
 * it. [observe] compares a snapshot with the last one it saw and queues one
 * event per change. The first time it sees a permission it queues it only
 * when granted: nothing was lost before MAMÁ looked. [pending] is what the
 * server has not confirmed yet, oldest first, and [confirm] drops what it
 * took. Everything lives in [kv], so a queued event survives the app dying.
 */
class PermissionLog(private val kv: KeyValueStore, private val maxPending: Int = MAX_PENDING) {

    /** Queues the changes since the last snapshot, at [atMs] (wall clock), and returns them. */
    @Synchronized
    fun observe(state: Map<String, Boolean>, atMs: Long): List<PermissionEvent> {
        val last = lastSeen()
        val at = Instant.ofEpochMilli(atMs).toString()
        val names = PermissionKeys.ALL.filter { it in state } + state.keys.filter { it !in PermissionKeys.ALL }.sorted()
        val changes = names.mapNotNull { name ->
            val granted = state.getValue(name)
            val before = last[name]
            if (before == granted || (before == null && !granted)) null else PermissionEvent(name, granted, at)
        }
        val seen = last + state
        if (seen != last) kv.putString(KEY_LAST, ContractJson.encodeToString(STATE, seen))
        if (changes.isNotEmpty()) savePending((pending() + changes).takeLast(maxPending))
        return changes
    }

    /** The events the server has not confirmed, oldest first. */
    @Synchronized
    fun pending(): List<PermissionEvent> = decode(KEY_PENDING, EVENTS).orEmpty()

    /** Drops [sent] from [pending]: the server stored them, or will never take them. */
    @Synchronized
    fun confirm(sent: List<PermissionEvent>) {
        val done = sent.toSet()
        savePending(pending().filterNot { it in done })
    }

    private fun lastSeen(): Map<String, Boolean> = decode(KEY_LAST, STATE).orEmpty()

    private fun savePending(events: List<PermissionEvent>) =
        kv.putString(KEY_PENDING, ContractJson.encodeToString(EVENTS, events))

    /** A value an older build saved in another shape reads as nothing, not as a crash. */
    private fun <T> decode(key: String, serializer: KSerializer<T>): T? {
        val raw = kv.getString(key) ?: return null
        return try {
            ContractJson.decodeFromString(serializer, raw)
        } catch (_: SerializationException) {
            null
        } catch (_: IllegalArgumentException) {
            null
        }
    }

    companion object {
        /** Never more queued than this: a phone offline for weeks keeps its newest changes. */
        const val MAX_PENDING = 100

        private const val KEY_LAST = "last_seen"
        private const val KEY_PENDING = "pending"
        private val STATE = MapSerializer(String.serializer(), Boolean.serializer())
        private val EVENTS = ListSerializer(PermissionEvent.serializer())
    }
}
