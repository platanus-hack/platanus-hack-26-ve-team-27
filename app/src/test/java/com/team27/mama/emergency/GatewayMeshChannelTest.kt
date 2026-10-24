package com.team27.mama.emergency

import com.team27.mama.mesh.MeshBroadcast
import com.team27.mama.mesh.MeshGateway
import com.team27.mama.mesh.MeshIdentity
import kotlinx.coroutines.CoroutineStart
import kotlinx.coroutines.async
import kotlinx.coroutines.flow.MutableSharedFlow
import kotlinx.coroutines.flow.first
import kotlinx.coroutines.test.UnconfinedTestDispatcher
import kotlinx.coroutines.test.runTest
import org.junit.Assert.assertEquals
import org.junit.Assert.assertFalse
import org.junit.Assert.assertTrue
import org.junit.Test

class GatewayMeshChannelTest {
    private class FakeGateway : MeshGateway {
        val flow = MutableSharedFlow<MeshBroadcast>(extraBufferCapacity = 8)
        val sent = mutableListOf<String>()
        var startResult = true
        var broadcastResult = true
        override val isRunning = false
        override val broadcasts = flow
        override val identity: MeshIdentity get() = error("unused")
        override fun start() = startResult
        override fun stop() = Unit
        override fun broadcast(text: String): Boolean {
            sent += text
            return broadcastResult
        }
    }

    @Test
    fun delegatesStartAndBroadcast() {
        val gateway = FakeGateway()
        val channel = GatewayMeshChannel(gateway)
        assertTrue(channel.start())
        gateway.startResult = false
        assertFalse(channel.start())
        gateway.broadcastResult = false
        assertFalse(channel.broadcast("MAMA1:a"))
        gateway.broadcastResult = true
        assertTrue(channel.broadcast("MAMA1:b"))
        assertEquals(listOf("MAMA1:a", "MAMA1:b"), gateway.sent)
    }

    @Test
    fun incomingMapsText() = runTest {
        val gateway = FakeGateway()
        val got = async(UnconfinedTestDispatcher(testScheduler), CoroutineStart.UNDISPATCHED) {
            GatewayMeshChannel(gateway).incoming.first()
        }
        gateway.flow.emit(MeshBroadcast("MAMA1:ACK:x", "0123456789abcdef", null, 6, 1, 0L))
        assertEquals("MAMA1:ACK:x", got.await())
    }
}
