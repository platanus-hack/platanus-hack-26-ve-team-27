package com.team27.mama.relay

import com.team27.mama.emergency.MeshEmergencyChannel
import com.team27.mama.mesh.MeshBroadcast
import com.team27.mama.mesh.MeshGateway
import com.team27.mama.mesh.MeshIdentity
import kotlinx.coroutines.flow.Flow
import kotlinx.coroutines.flow.MutableSharedFlow
import kotlinx.coroutines.flow.emptyFlow
import org.junit.Assert.assertEquals
import org.junit.Assert.assertTrue
import org.junit.Test

class MeshAdaptersTest {
    private class FakeIdentity(override val peerId: String) : MeshIdentity {
        override val publicKey: ByteArray = ByteArray(32)

        override fun sign(data: ByteArray): ByteArray = ByteArray(64)
    }

    private class FakeGateway : MeshGateway {
        val flow = MutableSharedFlow<MeshBroadcast>()
        var identityProvider: () -> MeshIdentity = { FakeIdentity("00000000000000aa") }
        val sent = mutableListOf<String>()
        override val isRunning = true
        override val broadcasts: Flow<MeshBroadcast> = flow
        override val identity: MeshIdentity get() = identityProvider()

        override fun start() = true

        override fun stop() = Unit

        override fun broadcast(text: String): Boolean {
            sent += text
            return true
        }
    }

    @Test
    fun gatewayRelayMeshDelegates() {
        val g = FakeGateway()
        val mesh = GatewayRelayMesh(g)
        assertEquals("00000000000000aa", mesh.ownPeerId)
        assertTrue(mesh.broadcast("MAMA1:ACK:x"))
        assertEquals(listOf("MAMA1:ACK:x"), g.sent)
        assertTrue(mesh.broadcasts === g.flow)
    }

    @Test
    fun ownPeerIdFailureIsSentinelAndRetried() {
        val g = FakeGateway()
        var fail = true
        g.identityProvider = { if (fail) throw IllegalStateException("keystore") else FakeIdentity("00000000000000bb") }
        val mesh = GatewayRelayMesh(g)
        assertEquals(GatewayRelayMesh.UNKNOWN_PEER, mesh.ownPeerId)
        fail = false
        assertEquals("00000000000000bb", mesh.ownPeerId)
        fail = true
        assertEquals("00000000000000bb", mesh.ownPeerId)
    }

    @Test
    fun ownTextChannelMarksBeforeBroadcast() {
        val calls = mutableListOf<String>()
        val flow = emptyFlow<String>()
        val inner = object : MeshEmergencyChannel {
            override fun start(): Boolean {
                calls += "start"
                return true
            }

            override fun broadcast(text: String): Boolean {
                calls += "send:$text"
                return true
            }

            override val incoming: Flow<String> = flow
        }
        val ch = OwnTextMeshChannel(inner) { calls += "own:$it" }
        assertTrue(ch.broadcast("MAMA1:SEAL:x"))
        assertTrue(ch.start())
        assertEquals(listOf("own:MAMA1:SEAL:x", "send:MAMA1:SEAL:x", "start"), calls)
        assertTrue(ch.incoming === flow)
    }
}
