package com.team27.mama.mesh

import android.content.Context
import com.team27.mama.mesh.crypto.MeshKeyStore
import kotlinx.coroutines.flow.Flow

/**
 * Public API of the bitchat BLE mesh: broadcast and receive `MAMA1:` texts over Bluetooth LE.
 *
 * - The caller holds the runtime permissions (BLUETOOTH_SCAN, BLUETOOTH_ADVERTISE, BLUETOOTH_CONNECT and
 *   location); without them [start] returns false.
 * - The mesh only runs while the process lives. Keeping it alive in the background is the foreground
 *   service of #60.
 * - [MeshBroadcast.relayHops] assumes the sender started at TTL 7, which is bitchat's default.
 */
interface MeshGateway {
    val isRunning: Boolean

    /** Received public broadcasts whose text starts with [MESSAGE_PREFIX]; each packet once. */
    val broadcasts: Flow<MeshBroadcast>

    /** This device's mesh identity. Reading it can throw if the key store is unusable. */
    val identity: MeshIdentity

    /** Starts BLE discovery + GATT. False if Bluetooth is off, unsupported or permissions are missing. Idempotent. */
    fun start(): Boolean

    /** Sends LEAVE and tears the node down. Idempotent. */
    fun stop()

    /**
     * Broadcasts [text] (must start with [MESSAGE_PREFIX]) with TTL 7, preceded by a fresh ANNOUNCE.
     * Returns false when not running. Throws IllegalArgumentException without the prefix.
     */
    fun broadcast(text: String): Boolean

    companion object {
        const val MESSAGE_PREFIX = "MAMA1:"

        @Volatile
        private var instance: MeshGateway? = null

        /** Process-wide singleton; uses context.applicationContext. Never throws: failures surface in [start]. */
        fun getInstance(context: Context): MeshGateway {
            instance?.let { return it }
            synchronized(this) {
                instance?.let { return it }
                val app = context.applicationContext
                // Lazy and serialised: two concurrent first calls cannot mint different identities, and a
                // Keystore failure becomes start() == false instead of a crash.
                val identity = LazyMeshIdentity { MeshKeyStore.loadOrCreate(MeshKeyStore.encryptedPrefs(app)) }
                val created = MeshGatewayImpl(identity) { listener ->
                    BleMeshNode(app, identity.crypto, listener)
                }
                instance = created
                return created
            }
        }
    }
}

interface MeshIdentity {
    /** Bitchat wire peer ID: 16 lowercase hex chars. */
    val peerId: String

    /** 32-byte Ed25519 public key (returns a copy). */
    val publicKey: ByteArray

    /** 64-byte Ed25519 signature over [data]. */
    fun sign(data: ByteArray): ByteArray
}

data class MeshBroadcast(
    /** Full text including "MAMA1:". */
    val text: String,
    /** 16 hex chars. */
    val senderPeerId: String,
    /** Sender's Ed25519 key from its verified ANNOUNCE, lowercase hex, null if unknown. */
    val senderPublicKeyHex: String?,
    /** TTL as received, 0..7. */
    val ttl: Int,
    /** Relays between sender and us: (7 - ttl).coerceIn(0, 7); 0 = direct neighbor. */
    val relayHops: Int,
    /** Sender's packet timestamp. */
    val timestampMs: Long,
)
