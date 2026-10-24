package com.team27.mama.mesh

/** Receives public MESSAGE text from verified bitchat peers. */
internal fun interface PublicMessageListener {
    fun onPublicMessage(
        senderPeerId: String,
        content: String,
        ttl: UByte,
        timestampMs: Long,
        senderSigningKey: ByteArray?
    )
}

/** One mesh run. A node is single-use: after [stop] it is never restarted. */
internal interface MeshNode {
    val myPeerId: String

    /** Starts BLE; false if Bluetooth is off, permissions are missing, or the node was used before. */
    fun start(): Boolean

    /** Sends LEAVE and shuts the node down. Idempotent. */
    fun stop()

    /** Broadcasts [text] as a signed public MESSAGE, preceded by a fresh ANNOUNCE. */
    fun sendBroadcast(text: String)
}
