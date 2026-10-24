package com.team27.mama.emergency

import kotlinx.coroutines.flow.Flow
import kotlinx.coroutines.flow.emptyFlow

/**
 * The server's `POST /emergencies` (#57). A 4xx reply must surface as
 * `ServerHttpException` with its code: it is permanent and never retried.
 */
interface HttpEmergencyChannel {
    /** POSTs [smsText] ("MAMA1 " + base64url signed payload); returns the server's mesh ack text. Throws IOException. */
    suspend fun post(smsText: String): String
}

/** Seam for #58. Retries/backoff (#62 §2.4: up to 3) belong to the implementation. */
interface SmsEmergencyChannel {
    /** Permission granted and cellular service present. */
    fun canSend(): Boolean

    suspend fun send(number: String, text: String): Boolean

    /** Raw SMS bodies from the central number. */
    val incoming: Flow<String>
}

/** No SMS sender yet (#58): SMS is never available. */
object NoSmsChannel : SmsEmergencyChannel {
    override fun canSend(): Boolean = false

    override suspend fun send(number: String, text: String): Boolean = false

    override val incoming: Flow<String> = emptyFlow()
}

/**
 * Forwards every call to the channel [current] returns at that moment, so a sender set after the
 * orchestrator was built (#58 assigning `graph.smsChannel`) still serves later emergencies. The
 * orchestrator reads [incoming] once per emergency, when it begins.
 */
internal class ForwardingSmsChannel(private val current: () -> SmsEmergencyChannel) : SmsEmergencyChannel {
    override fun canSend(): Boolean = current().canSend()

    override suspend fun send(number: String, text: String): Boolean = current().send(number, text)

    override val incoming: Flow<String> get() = current().incoming
}

/** The bitchat mesh. `broadcast() == true` means queued, not delivered: only an ack confirms. */
interface MeshEmergencyChannel {
    fun start(): Boolean

    fun broadcast(text: String): Boolean

    /** Texts of received `MAMA1:` broadcasts. */
    val incoming: Flow<String>
}

interface LocationSource {
    fun lastKnown(): LocationFix?

    /** Suspends until a new fix; null when location is unavailable. */
    suspend fun awaitFreshFix(): LocationFix?
}

/** Demo override of the channel order (#61). Read once per emergency, at declare. */
enum class ChannelOverride(val allowsHttp: Boolean, val allowsSms: Boolean, val allowsMesh: Boolean) {
    /** HTTP first, then SMS and mesh in parallel. */
    AUTO(allowsHttp = true, allowsSms = true, allowsMesh = true),
    NO_INTERNET(allowsHttp = false, allowsSms = true, allowsMesh = true),
    SMS_ONLY(allowsHttp = false, allowsSms = true, allowsMesh = false),
    BLE_ONLY(allowsHttp = false, allowsSms = false, allowsMesh = true),
}

class ChannelOverrideSetting {
    @Volatile
    var value: ChannelOverride = ChannelOverride.AUTO
}
