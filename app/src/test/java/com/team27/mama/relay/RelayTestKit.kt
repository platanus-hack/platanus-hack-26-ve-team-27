package com.team27.mama.relay

import com.team27.mama.emergency.payload.MamaText
import com.team27.mama.mesh.MeshBroadcast
import com.team27.mama.onboarding.NetworkStatus
import kotlinx.coroutines.CoroutineScope
import kotlinx.coroutines.ExperimentalCoroutinesApi
import kotlinx.coroutines.flow.MutableSharedFlow
import kotlinx.coroutines.test.TestScope
import kotlinx.coroutines.test.advanceTimeBy
import kotlinx.coroutines.test.runCurrent

/** Virtual time zero, as wall-clock ms. */
const val T0 = 1_790_000_000_000L

/** A well-formed SEAL text: a 146-byte box filled with [n]. */
fun seal(n: Int): String = MamaText.meshSealed(ByteArray(RelayLimits.SEALED_BOX_BYTES) { n.toByte() })

/** A well-formed server ack text (76 bytes). */
val ACK: String = MamaText.meshAck(ByteArray(RelayLimits.ACK_BYTES) { 4 })

fun heard(text: String, hops: Int = 0, sender: String = "0123456789abcdef") =
    MeshBroadcast(text, sender, null, 7 - hops, hops, 0L)

class FakeRelayMesh : RelayMesh {
    val incoming = MutableSharedFlow<MeshBroadcast>()
    override val broadcasts = incoming
    override val ownPeerId = "00000000000000aa"
    val sent = mutableListOf<String>()
    var broadcastResult = true

    override fun broadcast(text: String): Boolean {
        sent += text
        return broadcastResult
    }
}

class FakeUploader : RelayUploader {
    val calls = mutableListOf<Pair<String, Int>>()
    var behaviour: suspend (String) -> UploadResult = { UploadResult.Acked(ACK) }

    override suspend fun upload(text: String, hops: Int): UploadResult {
        calls += text to hops
        return behaviour(text)
    }
}

class FakeNetwork : NetworkStatus {
    var online = false
    override fun isOnline(): Boolean = online
}

class InMemoryRelayStore : RelayStore {
    var json: String? = null
    var saves = 0

    override fun load(): String? = json

    override fun save(json: String) {
        saves++
        this.json = json
    }
}

/** The fakes of one relay; build it with [relay] inside `runTest`. Pass [store] to restart over another kit's state. */
@OptIn(ExperimentalCoroutinesApi::class)
class RelayTestKit(val store: InMemoryRelayStore = InMemoryRelayStore()) {
    val mesh = FakeRelayMesh()
    val uploader = FakeUploader()
    val network = FakeNetwork()

    /** Starts a relay on [scope] (default `backgroundScope`) so its collector is subscribed before any emit. */
    fun TestScope.relay(scope: CoroutineScope = backgroundScope): MeshRelay =
        MeshRelay(
            mesh = mesh,
            uploader = uploader,
            network = network,
            store = store,
            scope = scope,
            nowMs = { T0 + testScheduler.currentTime },
        ).also {
            it.start()
            runCurrent()
        }

    /** Delivers [broadcast] to the relay and runs what it triggers. */
    suspend fun TestScope.hear(broadcast: MeshBroadcast) {
        mesh.incoming.emit(broadcast)
        runCurrent()
    }

    /** The persisted snapshot, or null when nothing was saved. */
    internal fun saved(): RelaySnapshot? = store.json?.let { RelayJson.decodeFromString(RelaySnapshot.serializer(), it) }

    /** Puts [snapshot] in the store, as a previous run would have left it. */
    internal fun stored(snapshot: RelaySnapshot) {
        store.json = RelayJson.encodeToString(RelaySnapshot.serializer(), snapshot)
    }

    /** Advances virtual time to exactly [t] ms after the test started and runs what is due. */
    fun TestScope.at(t: Long) {
        advanceTimeBy(t - testScheduler.currentTime)
        runCurrent()
    }
}
