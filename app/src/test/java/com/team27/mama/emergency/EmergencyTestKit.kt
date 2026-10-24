package com.team27.mama.emergency

import com.team27.mama.emergency.config.EmergencyConfig
import com.team27.mama.emergency.payload.AckCodec
import com.team27.mama.emergency.payload.AckInfo
import com.team27.mama.emergency.payload.Ed25519SeedSigner
import com.team27.mama.emergency.payload.EmergencyPayload
import com.team27.mama.emergency.payload.MamaText
import com.team27.mama.emergency.payload.MeshFrame
import com.team27.mama.emergency.payload.PayloadCodec
import com.team27.mama.emergency.payload.PayloadSigner
import com.team27.mama.emergency.payload.SealedBox
import java.io.IOException
import kotlinx.coroutines.CompletableDeferred
import kotlinx.coroutines.ExperimentalCoroutinesApi
import kotlinx.coroutines.flow.MutableSharedFlow
import kotlinx.coroutines.test.TestScope

class FakeHttp : HttpEmergencyChannel {
    val posted = mutableListOf<String>()
    var behaviour: suspend (String) -> String = { throw IOException("offline") }

    override suspend fun post(smsText: String): String {
        posted += smsText
        return behaviour(smsText)
    }
}

class FakeSms : SmsEmergencyChannel {
    var canSend = true
    val sent = mutableListOf<Pair<String, String>>()
    var behaviour: suspend (String, String) -> Boolean = { _, _ -> true }
    override val incoming = MutableSharedFlow<String>()

    override fun canSend(): Boolean = canSend

    override suspend fun send(number: String, text: String): Boolean {
        sent += number to text
        return behaviour(number, text)
    }
}

class FakeMesh : MeshEmergencyChannel {
    var startResult = true
    var broadcastResult = true
    var startCalls = 0
    val broadcasts = mutableListOf<String>()
    override val incoming = MutableSharedFlow<String>()

    override fun start(): Boolean {
        startCalls++
        return startResult
    }

    override fun broadcast(text: String): Boolean {
        broadcasts += text
        return broadcastResult
    }
}

class FakeLocation : LocationSource {
    var lastKnown: LocationFix? = null
    val fresh = CompletableDeferred<LocationFix?>()

    override fun lastKnown(): LocationFix? = lastKnown

    override suspend fun awaitFreshFix(): LocationFix? = fresh.await()
}

/** Fakes for every port, plus helpers to read what the orchestrator sent. One kit per run. */
class EmergencyTestKit {
    val http = FakeHttp()
    val sms = FakeSms()
    val mesh = FakeMesh()
    val location = FakeLocation()
    var online = false
    var config = EmergencyConfig(SMS_NUMBER, SERVER.publicKey, SERVER_X)

    @OptIn(ExperimentalCoroutinesApi::class)
    fun TestScope.orchestrator(
        override: ChannelOverride = ChannelOverride.AUTO,
        signer: PayloadSigner = DEVICE,
    ) = EmergencyOrchestrator(
        signer = signer,
        config = { config },
        http = http,
        sms = sms,
        mesh = mesh,
        location = location,
        network = { online },
        channelOverride = { override },
        scope = backgroundScope,
        nowMs = { NOW + testScheduler.currentTime },
        newEmergencyId = { 42L },
    )

    companion object {
        const val SMS_NUMBER = "+15555550100"
        const val NOW = 1_790_000_000_000L
        val DEVICE = Ed25519SeedSigner(ByteArray(32) { 1 })
        val SERVER = Ed25519SeedSigner(ByteArray(32) { 7 })
        val SERVER_X_PRIVATE = ByteArray(32) { 9 }
        val SERVER_X: ByteArray = SealedBox.publicKeyOf(SERVER_X_PRIVATE)

        fun decodeSms(text: String): EmergencyPayload =
            PayloadCodec.verify(MamaText.parseSms(text), DEVICE.publicKey)

        fun decodeMesh(text: String): EmergencyPayload {
            val box = (MamaText.parseMesh(text) as MeshFrame.Sealed).box
            return PayloadCodec.verify(SealedBox.open(box, SERVER_X_PRIVATE), DEVICE.publicKey)
        }

        private fun ack(id: Long, seq: Int) = AckCodec.sign(AckInfo(id, seq), SERVER)

        fun meshAck(id: Long, seq: Int): String = MamaText.meshAck(ack(id, seq))

        fun smsAck(id: Long, seq: Int): String = MamaText.sms(ack(id, seq))

        /** The server answers POST /emergencies with the mesh ack text. */
        fun httpAck(id: Long, seq: Int): String = meshAck(id, seq)
    }
}
