package com.team27.mama.emergency.sms

import com.team27.mama.emergency.payload.AckInfo
import java.util.concurrent.CopyOnWriteArrayList

/**
 * Where verified server acks land, whatever channel carried them. The SMS listener
 * ([SmsAckHandler]) publishes here; the SMS channel stops on a match (AppGraph wires
 * [SmsChannel.onAck]). #59 subscribes too, to stop the mesh and HTTP for the same emergency.
 *
 * Listeners run synchronously on the publishing thread (a broadcast receiver's main thread),
 * so they must return quickly and never throw.
 */
class OfflineAcks {
    private val listeners = CopyOnWriteArrayList<(AckInfo) -> Unit>()

    /** Returns a function that removes the listener. */
    fun subscribe(listener: (AckInfo) -> Unit): () -> Unit {
        listeners += listener
        return { listeners -= listener }
    }

    fun publish(ack: AckInfo) {
        listeners.forEach { it(ack) }
    }
}
