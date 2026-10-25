package com.team27.mama.emergency

import com.team27.mama.mesh.MeshGateway
import kotlinx.coroutines.flow.Flow
import kotlinx.coroutines.flow.map

/** Adapts the #56 [MeshGateway] to the orchestrator's port. Consumer: `AppGraph.emergency`. */
class GatewayMeshChannel(private val gateway: MeshGateway) : MeshEmergencyChannel {
    override fun start(): Boolean = gateway.start()

    override fun broadcast(text: String): Boolean = gateway.broadcast(text)

    override val incoming: Flow<String> = gateway.broadcasts.map { it.text }
}
