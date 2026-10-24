package com.team27.mama.mesh.crypto

import org.junit.Assert.assertArrayEquals
import org.junit.Assert.assertEquals
import org.junit.Assert.assertNotEquals
import org.junit.Assert.assertNotNull
import org.junit.Test
import java.util.Base64

/** The mesh identity survives restarts: #54/#57 register its Ed25519 public key with the server. */
class MeshKeyStoreTest {

    private class MapStore : KeyValueStore {
        val map = HashMap<String, String>()
        override fun getString(key: String): String? = map[key]
        override fun putString(key: String, value: String) {
            map[key] = value
        }
    }

    @Test
    fun loadOrCreate_isStableAcrossLoads() {
        val store = MapStore()
        val a = MeshKeyStore.loadOrCreate(store)
        val b = MeshKeyStore.loadOrCreate(store)
        assertEquals(a.myPeerID, b.myPeerID)
        assertArrayEquals(a.getSigningPublicKey(), b.getSigningPublicKey())
        assertArrayEquals(a.getStaticPublicKey(), b.getStaticPublicKey())
        assertNotNull(store.getString(MeshKeyStore.KEY_STATIC_PRIVATE))
        assertNotNull(store.getString(MeshKeyStore.KEY_SIGNING_PRIVATE))
    }

    @Test
    fun corruptStoredKey_isReplacedWithFreshKeys() {
        val store = MapStore()
        store.putString(MeshKeyStore.KEY_STATIC_PRIVATE, "not-base64")
        store.putString(MeshKeyStore.KEY_SIGNING_PRIVATE, "not-base64")
        val svc = MeshKeyStore.loadOrCreate(store)
        assertStoredKeyIsValid(store, MeshKeyStore.KEY_STATIC_PRIVATE)
        assertStoredKeyIsValid(store, MeshKeyStore.KEY_SIGNING_PRIVATE)
        assertEquals(svc.myPeerID, MeshKeyStore.loadOrCreate(store).myPeerID)
    }

    @Test
    fun wrongLengthKey_replacesBoth() {
        val store = MapStore()
        val original = MeshKeyStore.loadOrCreate(store)
        val signingBefore = store.getString(MeshKeyStore.KEY_SIGNING_PRIVATE)
        store.putString(MeshKeyStore.KEY_STATIC_PRIVATE, Base64.getEncoder().encodeToString(ByteArray(16)))
        val replaced = MeshKeyStore.loadOrCreate(store)
        assertNotEquals(original.myPeerID, replaced.myPeerID)
        assertNotEquals(signingBefore, store.getString(MeshKeyStore.KEY_SIGNING_PRIVATE))
        assertStoredKeyIsValid(store, MeshKeyStore.KEY_STATIC_PRIVATE)
    }

    @Test
    fun emptyStore_createsDistinctIdentities() {
        val a = MeshKeyStore.loadOrCreate(MapStore())
        val b = MeshKeyStore.loadOrCreate(MapStore())
        assertNotEquals(a.myPeerID, b.myPeerID)
    }

    private fun assertStoredKeyIsValid(store: MapStore, key: String) {
        val bytes = Base64.getDecoder().decode(store.getString(key))
        assertEquals(EncryptionService.KEY_SIZE, bytes.size)
    }

    @Test
    fun loadOrCreate_writesBothKeysInOneBatch() {
        var batches = 0
        val store = object : KeyValueStore {
            val map = HashMap<String, String>()
            override fun getString(key: String): String? = map[key]
            override fun putString(key: String, value: String) { map[key] = value }
            override fun putStrings(values: Map<String, String>) { batches++; map.putAll(values) }
        }
        MeshKeyStore.loadOrCreate(store)
        assertEquals(1, batches)
        assertEquals(2, store.map.size)
    }
}
