package com.team27.mama.emergency.config

import com.team27.mama.contract.ContractJson
import com.team27.mama.contract.RemoteConfig
import com.team27.mama.server.ServerApi
import java.io.IOException
import java.util.Base64

interface ConfigStore {
    fun read(): String?
    fun write(json: String)
}

/**
 * Keys are null until the first successful fetch: callers (#59) must not verify
 * acks or seal payloads without them.
 */
class EmergencyConfig(
    val smsNumber: String,
    val serverEd25519PublicKey: ByteArray?,
    val serverX25519PublicKey: ByteArray?,
)

class RemoteConfigRepository(
    private val api: ServerApi,
    private val store: ConfigStore,
    private val defaultSmsNumber: String,
) {
    private val fallback = EmergencyConfig(defaultSmsNumber, null, null)

    /** Cached config, or the build-time default. Never throws. */
    fun current(): EmergencyConfig = try {
        store.read()?.let(::parse) ?: fallback
    } catch (e: Exception) {
        fallback
    }

    /** True when a fresh config was fetched, validated and cached. */
    fun refresh(): Boolean = try {
        val fetched = api.fetchConfig()
        if (toConfig(fetched) == null) {
            false
        } else {
            store.write(ContractJson.encodeToString(RemoteConfig.serializer(), fetched))
            true
        }
    } catch (e: IOException) {
        false
    } catch (e: RuntimeException) {
        false
    }

    private fun parse(json: String): EmergencyConfig? =
        toConfig(ContractJson.decodeFromString(RemoteConfig.serializer(), json))

    private fun toConfig(rc: RemoteConfig): EmergencyConfig? {
        val ed = key(rc.serverEd25519PublicKey) ?: return null
        val x = key(rc.serverX25519PublicKey) ?: return null
        val number = rc.smsNumber?.trim().takeUnless { it.isNullOrEmpty() } ?: defaultSmsNumber
        return EmergencyConfig(number, ed, x)
    }

    private fun key(b64: String): ByteArray? =
        try {
            Base64.getDecoder().decode(b64).takeIf { it.size == KEY_SIZE }
        } catch (e: IllegalArgumentException) {
            null
        }

    private companion object {
        const val KEY_SIZE = 32
    }
}
