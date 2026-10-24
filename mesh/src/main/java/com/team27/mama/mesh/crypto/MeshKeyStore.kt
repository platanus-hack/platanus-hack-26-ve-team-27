// Derived from bitchat-android crypto/EncryptionService.kt @ dfc67da (GPL-3.0).
// Upstream: https://github.com/permissionlesstech/bitchat-android commit
// dfc67dad41691128bc6e7425bd69f2f409cb5bbf, file app/src/main/java/com/bitchat/android/crypto/EncryptionService.kt.
// GPL-3.0: see LICENSE and mesh/NOTICE.md.
// MAMÁ changes: only key persistence is kept (setUpEncryptedPrefs, same schemes, our own prefs file);
// both private keys are stored as Base64 and regenerated together when either is missing or invalid;
// a failed write throws instead of returning an identity that was never saved.

package com.team27.mama.mesh.crypto

import android.content.Context
import android.util.Log
import androidx.security.crypto.EncryptedSharedPreferences
import androidx.security.crypto.MasterKey
import java.security.SecureRandom
import java.util.Base64

/** Minimal string storage, so key persistence is testable without Android. */
internal interface KeyValueStore {
    fun getString(key: String): String?
    fun putString(key: String, value: String)

    /**
     * Writes all [values] together; implementations should make this a single atomic write.
     * Returns false when the write did not reach storage.
     */
    fun putStrings(values: Map<String, String>): Boolean {
        values.forEach { (k, v) -> putString(k, v) }
        return true
    }
}

/**
 * Loads this device's mesh identity, creating it on first use. The identity must stay stable across
 * app restarts because the Ed25519 public key is registered with the MAMÁ server.
 */
internal object MeshKeyStore {
    private const val TAG = "MeshKeyStore"

    const val PREFS_NAME = "mama_mesh_identity"
    const val KEY_STATIC_PRIVATE = "x25519_static_private_key"
    const val KEY_SIGNING_PRIVATE = "ed25519_signing_private_key"

    /** Throws IllegalStateException when a new identity cannot be persisted; nothing unsaved is returned. */
    fun loadOrCreate(store: KeyValueStore, random: SecureRandom = SecureRandom()): EncryptionService {
        val staticKey = decode(store.getString(KEY_STATIC_PRIVATE))
        val signingKey = decode(store.getString(KEY_SIGNING_PRIVATE))
        if (staticKey != null && signingKey != null) {
            return EncryptionService(staticKey, signingKey)
        }

        Log.i(TAG, "No valid stored mesh identity; generating a new one")
        val (freshStatic, freshSigning) = EncryptionService.generatePrivateKeys(random)
        val encoder = Base64.getEncoder()
        val saved = store.putStrings(
            mapOf(
                KEY_STATIC_PRIVATE to encoder.encodeToString(freshStatic),
                KEY_SIGNING_PRIVATE to encoder.encodeToString(freshSigning)
            )
        )
        // An unsaved identity would change on the next start while the server holds the old public key.
        check(saved) { "Failed to persist mesh identity" }
        return EncryptionService(freshStatic, freshSigning)
    }

    /**
     * EncryptedSharedPreferences set up exactly as upstream `setUpEncryptedPrefs`, in [PREFS_NAME].
     * security-crypto 1.1.0 deprecates this API; it is kept to match upstream's key storage.
     */
    @Suppress("DEPRECATION")
    fun encryptedPrefs(context: Context): KeyValueStore {
        val appContext = context.applicationContext
        val masterKey = MasterKey.Builder(appContext, MasterKey.DEFAULT_MASTER_KEY_ALIAS)
            .setKeyScheme(MasterKey.KeyScheme.AES256_GCM)
            .build()
        val prefs = EncryptedSharedPreferences.create(
            appContext,
            PREFS_NAME,
            masterKey,
            EncryptedSharedPreferences.PrefKeyEncryptionScheme.AES256_SIV,
            EncryptedSharedPreferences.PrefValueEncryptionScheme.AES256_GCM
        )
        return object : KeyValueStore {
            override fun getString(key: String): String? = prefs.getString(key, null)

            override fun putString(key: String, value: String) {
                check(putStrings(mapOf(key to value))) { "Failed to persist $key" }
            }

            // One commit(), not apply(): the identity must be on disk, both keys together, before it is announced.
            override fun putStrings(values: Map<String, String>): Boolean {
                val editor = prefs.edit()
                values.forEach { (k, v) -> editor.putString(k, v) }
                val committed = editor.commit()
                if (!committed) Log.e(TAG, "Failed to persist mesh identity: commit() returned false")
                return committed
            }
        }
    }

    private fun decode(value: String?): ByteArray? {
        if (value == null) return null
        val bytes = try {
            Base64.getDecoder().decode(value)
        } catch (_: IllegalArgumentException) {
            return null
        }
        return bytes.takeIf { it.size == EncryptionService.KEY_SIZE }
    }
}
