package com.team27.mama.relay

import com.team27.mama.emergency.MeshEmergencyChannel
import com.team27.mama.mesh.MeshBroadcast
import com.team27.mama.mesh.MeshGateway
import kotlinx.coroutines.flow.Flow

/**
 * [RelayMesh] over the BLE gateway. [ownPeerId] reads `gateway.identity.peerId` on first use; if the
 * identity is unusable it returns [UNKNOWN_PEER] (matches no real peer) and tries again next time.
 */
class GatewayRelayMesh(private val gateway: MeshGateway) : RelayMesh {
    private var cachedPeerId: String? = null

    override val broadcasts: Flow<MeshBroadcast> get() = gateway.broadcasts

    override val ownPeerId: String
        @Synchronized get() {
            cachedPeerId?.let { return it }
            return try {
                gateway.identity.peerId.also { cachedPeerId = it }
            } catch (e: Exception) {
                UNKNOWN_PEER
            }
        }

    override fun broadcast(text: String): Boolean = gateway.broadcast(text)

    companion object {
        const val UNKNOWN_PEER = ""
    }
}

/** Tells the relay about every text the orchestrator sends, so it never uploads our own emergency. */
class OwnTextMeshChannel(
    private val inner: MeshEmergencyChannel,
    private val markOwn: (String) -> Unit,
) : MeshEmergencyChannel {
    override fun start(): Boolean = inner.start()

    override fun broadcast(text: String): Boolean {
        markOwn(text)
        return inner.broadcast(text)
    }

    override val incoming: Flow<String> get() = inner.incoming
}
