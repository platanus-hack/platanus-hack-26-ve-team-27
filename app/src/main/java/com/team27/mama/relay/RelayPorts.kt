package com.team27.mama.relay

import com.team27.mama.mesh.MeshBroadcast
import kotlinx.coroutines.flow.Flow

/** The slice of the BLE mesh the relay uses; Task 5 adapts `MeshGateway` to it. */
interface RelayMesh {
    /** Every MAMA1 broadcast heard from another phone (hot, no replay). */
    val broadcasts: Flow<MeshBroadcast>

    /** This phone's mesh peer id (16 hex chars). */
    val ownPeerId: String

    /** Queues [text] for the mesh; false when the mesh refused it. */
    fun broadcast(text: String): Boolean
}

/** One upload's outcome; see `POST /emergencies/relay` (#57). */
sealed interface UploadResult {
    /** 200 with a `MAMA1:ACK:` body: delivered. */
    data class Acked(val ackText: String) : UploadResult

    /** 4xx: permanent, never retried. */
    data object Rejected : UploadResult

    /** 5xx, network or a bad reply: retried with backoff. */
    data object Failed : UploadResult
}

/** Sends a heard SEAL text, verbatim, to the server. */
fun interface RelayUploader {
    suspend fun upload(text: String, hops: Int): UploadResult
}

/** Where the relay keeps its [RelaySnapshot] JSON. */
interface RelayStore {
    fun load(): String?

    fun save(json: String)
}
