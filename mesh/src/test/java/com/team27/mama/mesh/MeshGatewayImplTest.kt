package com.team27.mama.mesh

import com.team27.mama.mesh.MeshGatewayImpl.Companion.toBroadcastOrNull
import kotlinx.coroutines.CoroutineStart
import kotlinx.coroutines.async
import kotlinx.coroutines.flow.first
import kotlinx.coroutines.runBlocking
import org.junit.Assert.assertEquals
import org.junit.Assert.assertFalse
import org.junit.Assert.assertNotNull
import org.junit.Assert.assertNull
import org.junit.Assert.assertThrows
import org.junit.Assert.assertTrue
import org.junit.Test

class MeshGatewayImplTest {
    private val id = "0123456789abcdef"

    private class FakeIdentity : MeshIdentity {
        override val peerId = "0123456789abcdef"
        override val publicKey = ByteArray(32)
        override fun sign(data: ByteArray) = ByteArray(64)
    }

    private class FakeNode(private val startResult: Boolean = true) : MeshNode {
        override val myPeerId = "0123456789abcdef"
        val sent = mutableListOf<String>()
        var startCalls = 0
        var stopCalls = 0
        override fun start(): Boolean { startCalls++; return startResult }
        override fun stop() { stopCalls++ }
        override fun sendBroadcast(text: String) { sent += text }
    }

    private class Harness(vararg results: Boolean) {
        val nodes = mutableListOf<FakeNode>()
        var listener: PublicMessageListener? = null
        private val queue = results.toMutableList()
        val gateway = MeshGatewayImpl(FakeIdentity()) { l ->
            listener = l
            FakeNode(if (queue.isEmpty()) true else queue.removeAt(0)).also { nodes += it }
        }
    }

    @Test fun prefixFilter() {
        assertNotNull(toBroadcastOrNull(id, "MAMA1:abc", 7u, 1L, null))
        assertNull(toBroadcastOrNull(id, "mama1:abc", 7u, 1L, null))
        assertNull(toBroadcastOrNull(id, "MAMA1 abc", 7u, 1L, null))
        assertNull(toBroadcastOrNull(id, "hola", 7u, 1L, null))
    }

    @Test fun hopsFromTtl() {
        assertEquals(0, toBroadcastOrNull(id, "MAMA1:x", 7u, 1L, null)!!.relayHops)
        assertEquals(2, toBroadcastOrNull(id, "MAMA1:x", 5u, 1L, null)!!.relayHops)
        assertEquals(7, toBroadcastOrNull(id, "MAMA1:x", 0u, 1L, null)!!.relayHops)
        assertEquals(0, toBroadcastOrNull(id, "MAMA1:x", 9u, 1L, null)!!.relayHops)
    }

    @Test fun ttlIsClampedToSeven() {
        assertEquals(7, toBroadcastOrNull(id, "MAMA1:x", 9u, 1L, null)!!.ttl)
        assertEquals(7, toBroadcastOrNull(id, "MAMA1:x", 255u, 1L, null)!!.ttl)
        assertEquals(5, toBroadcastOrNull(id, "MAMA1:x", 5u, 1L, null)!!.ttl)
        assertEquals(0, toBroadcastOrNull(id, "MAMA1:x", 0u, 1L, null)!!.ttl)
    }

    @Test fun signingKeyIsHex() {
        val b = toBroadcastOrNull(id, "MAMA1:x", 7u, 1L, byteArrayOf(0x0a, 0xff.toByte()))!!
        assertEquals("0aff", b.senderPublicKeyHex)
    }

    @Test fun startReturnsFalseWhenNodeFailsToStart() {
        val h = Harness(false)
        assertFalse(h.gateway.start())
        assertFalse(h.gateway.isRunning)
        assertEquals(1, h.nodes[0].stopCalls)
    }

    @Test fun startReturnsFalseWhenFactoryThrows() {
        val g = MeshGatewayImpl(FakeIdentity()) { throw IllegalStateException("no bluetooth service") }
        assertFalse(g.start())
        assertFalse(g.isRunning)
        assertFalse(g.broadcast("MAMA1:x"))
    }

    @Test fun startReturnsFalseWhenNodeStartThrows() {
        val node = object : MeshNode {
            var stops = 0
            override val myPeerId = "0123456789abcdef"
            override fun start(): Boolean = throw SecurityException("permission")
            override fun stop() { stops++ }
            override fun sendBroadcast(text: String) {}
        }
        val g = MeshGatewayImpl(FakeIdentity()) { node }
        assertFalse(g.start())
        assertFalse(g.isRunning)
        assertEquals(1, node.stops)
    }

    @Test fun startIsIdempotent() {
        val h = Harness()
        assertTrue(h.gateway.start())
        assertTrue(h.gateway.start())
        assertEquals(1, h.nodes.size)
    }

    @Test fun broadcastWhenStopped_returnsFalse() {
        val h = Harness()
        assertFalse(h.gateway.broadcast("MAMA1:x"))
        assertTrue(h.nodes.isEmpty())
    }

    @Test fun broadcastWithoutPrefix_throws() {
        val h = Harness()
        h.gateway.start()
        assertThrows(IllegalArgumentException::class.java) { h.gateway.broadcast("hola") }
    }

    @Test fun broadcastWhenRunning_delegates() {
        val h = Harness()
        h.gateway.start()
        assertTrue(h.gateway.broadcast("MAMA1:x"))
        assertEquals(listOf("MAMA1:x"), h.nodes[0].sent)
    }

    @Test fun restartCreatesFreshNode() {
        val h = Harness()
        h.gateway.start()
        h.gateway.stop()
        h.gateway.stop()
        assertTrue(h.gateway.start())
        assertEquals(2, h.nodes.size)
        assertEquals(1, h.nodes[0].stopCalls)
        assertFalse(h.nodes[0] === h.nodes[1])
    }

    @Test fun listenerEmitsOnlyPrefixed() = runBlocking {
        val h = Harness()
        h.gateway.start()
        val got = async(start = CoroutineStart.UNDISPATCHED) { h.gateway.broadcasts.first() }
        h.listener!!.onPublicMessage(id, "hola", 7u, 1L, null)
        h.listener!!.onPublicMessage(id, "MAMA1:x", 6u, 2L, null)
        assertEquals(MeshBroadcast("MAMA1:x", id, null, 6, 1, 2L), got.await())
    }

    @Test fun lazyIdentity_loadsOnceUnderConcurrency() {
        var loads = 0
        val identity = LazyMeshIdentity {
            loads++
            Thread.sleep(50)
            com.team27.mama.mesh.crypto.MeshKeyStore.loadOrCreate(MapStore())
        }
        val ids = java.util.Collections.synchronizedList(mutableListOf<String>())
        val threads = List(4) { Thread { ids += identity.peerId } }
        threads.forEach { it.start() }
        threads.forEach { it.join() }
        assertEquals(1, loads)
        assertEquals(1, ids.toSet().size)
        assertEquals(16, ids[0].length)
        assertEquals(32, identity.publicKey.size)
        assertEquals(64, identity.sign(byteArrayOf(1)).size)
    }

    @Test fun lazyIdentity_failureThenRetry() {
        var attempts = 0
        val identity = LazyMeshIdentity {
            if (attempts++ == 0) throw IllegalStateException("keystore") 
            com.team27.mama.mesh.crypto.MeshKeyStore.loadOrCreate(MapStore())
        }
        assertThrows(IllegalStateException::class.java) { identity.peerId }
        assertEquals(16, identity.peerId.length)
    }

    @Test fun startReturnsFalseWhenIdentityLoadFails() {
        val identity = LazyMeshIdentity { throw IllegalStateException("keystore") }
        val g = MeshGatewayImpl(identity) { identity.peerId; FakeNode() }
        assertFalse(g.start())
        assertFalse(g.isRunning)
    }

    @Test fun failedIdentityWrite_startFalseThenRetrySucceeds() {
        var commits = 0
        val store = object : com.team27.mama.mesh.crypto.KeyValueStore {
            val map = HashMap<String, String>()
            override fun getString(key: String): String? = map[key]
            override fun putString(key: String, value: String) { map[key] = value }
            override fun putStrings(values: Map<String, String>): Boolean {
                if (commits++ == 0) return false // first commit() fails, nothing written
                map.putAll(values)
                return true
            }
        }
        val identity = LazyMeshIdentity { com.team27.mama.mesh.crypto.MeshKeyStore.loadOrCreate(store) }
        val g = MeshGatewayImpl(identity) { identity.peerId; FakeNode() }
        assertFalse(g.start())
        assertTrue(store.map.isEmpty())
        assertTrue(g.start())
        assertEquals(2, store.map.size)
        assertEquals(identity.peerId, com.team27.mama.mesh.crypto.MeshKeyStore.loadOrCreate(store).myPeerID)
    }

    private class MapStore : com.team27.mama.mesh.crypto.KeyValueStore {
        val map = HashMap<String, String>()
        override fun getString(key: String): String? = map[key]
        override fun putString(key: String, value: String) { map[key] = value }
    }
}
