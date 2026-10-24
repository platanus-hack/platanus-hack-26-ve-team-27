package com.team27.mama.mesh

import android.util.Log
import com.team27.mama.mesh.crypto.EncryptionService
import com.team27.mama.mesh.util.AppConstants
import kotlinx.coroutines.channels.BufferOverflow
import kotlinx.coroutines.flow.Flow
import kotlinx.coroutines.flow.MutableSharedFlow
import kotlinx.coroutines.flow.asSharedFlow

/** [MeshIdentity] over an identity loaded on first use. A failed load is retried on the next access. */
internal class LazyMeshIdentity(loader: () -> EncryptionService) : MeshIdentity {
    // SYNCHRONIZED lazy: concurrent first accesses load (and persist) the identity once; exceptions are not cached.
    val crypto: EncryptionService by lazy(LazyThreadSafetyMode.SYNCHRONIZED, loader)

    override val peerId: String get() = crypto.myPeerID
    override val publicKey: ByteArray get() = crypto.getSigningPublicKey().copyOf()
    override fun sign(data: ByteArray): ByteArray = checkNotNull(crypto.signData(data)) { "signing failed" }
}

internal class MeshGatewayImpl(
    override val identity: MeshIdentity,
    private val nodeFactory: (PublicMessageListener) -> MeshNode,
) : MeshGateway {

    companion object {
        private const val TAG = "MeshGateway"

        internal fun toBroadcastOrNull(
            senderPeerId: String,
            content: String,
            ttl: UByte,
            timestampMs: Long,
            senderSigningKey: ByteArray?
        ): MeshBroadcast? {
            if (!content.startsWith(MeshGateway.MESSAGE_PREFIX)) return null
            val max = AppConstants.MESSAGE_TTL_HOPS.toInt()
            val ttlInt = ttl.toInt().coerceIn(0, max)
            return MeshBroadcast(
                text = content,
                senderPeerId = senderPeerId,
                senderPublicKeyHex = senderSigningKey?.joinToString("") { "%02x".format(it) },
                ttl = ttlInt,
                relayHops = max - ttlInt,
                timestampMs = timestampMs
            )
        }
    }

    private val flow = MutableSharedFlow<MeshBroadcast>(
        replay = 0,
        extraBufferCapacity = 64,
        onBufferOverflow = BufferOverflow.DROP_OLDEST
    )
    override val broadcasts: Flow<MeshBroadcast> = flow.asSharedFlow()

    private val listener = PublicMessageListener { peer, content, ttl, ts, key ->
        toBroadcastOrNull(peer, content, ttl, ts, key)?.let { flow.tryEmit(it) }
    }

    private var node: MeshNode? = null

    override val isRunning: Boolean
        @Synchronized get() = node != null

    @Synchronized
    override fun start(): Boolean {
        if (node != null) return true
        var candidate: MeshNode? = null
        try {
            candidate = nodeFactory(listener)
            if (candidate.start()) {
                node = candidate
                return true
            }
        } catch (e: Exception) {
            // No Bluetooth service, Keystore failure, missing permission: report "not started".
            Log.e(TAG, "Mesh start failed: ${e.message}")
        }
        // Release the component scopes of a node that never ran.
        try {
            candidate?.stop()
        } catch (e: Exception) {
            Log.w(TAG, "Cleanup failed: ${e.message}")
        }
        return false
    }

    @Synchronized
    override fun stop() {
        val current = node ?: return
        node = null
        current.stop()
    }

    @Synchronized
    override fun broadcast(text: String): Boolean {
        require(text.startsWith(MeshGateway.MESSAGE_PREFIX)) { "text must start with ${MeshGateway.MESSAGE_PREFIX}" }
        val current = node ?: return false
        current.sendBroadcast(text)
        return true
    }
}
