package com.team27.mama.emergency.sms

import com.team27.mama.contract.DeviceRegistration
import com.team27.mama.contract.RemoteConfig
import com.team27.mama.emergency.config.InMemoryConfigStore
import com.team27.mama.emergency.config.RemoteConfigRepository
import com.team27.mama.emergency.payload.Vectors
import com.team27.mama.emergency.payload.unhex
import com.team27.mama.server.ServerApi
import kotlinx.coroutines.CancellationException
import kotlinx.coroutines.Deferred
import kotlinx.coroutines.runBlocking
import org.junit.Assert.assertEquals
import org.junit.Assert.assertTrue
import org.junit.Test

class SmsChannelTest {
    private val signed = Vectors.file.payloads.first().signed.unhex()
    private val expectedText = Vectors.file.payloads.first().sms
    private val number = "+15555550100"

    private object NoApi : ServerApi {
        override fun fetchConfig(): RemoteConfig = error("offline")
        override fun registerDevice(registration: DeviceRegistration) = Unit
    }

    private fun repo(default: String) = RemoteConfigRepository(NoApi, InMemoryConfigStore(), default)

    /** Replies from [results] in order, then false; records every (to, text). */
    private class FakeSender(var results: List<Boolean>, var permitted: Boolean = true) : SmsSender {
        val sent = mutableListOf<Pair<String, String>>()
        var onSend: () -> Unit = {}
        override fun canSend() = permitted
        override suspend fun sendText(to: String, text: String): Boolean {
            sent += to to text
            onSend()
            return results.getOrElse(sent.size - 1) { false }
        }
    }

    private val slept = mutableListOf<Long>()
    private val sleeper: suspend (Long, Deferred<Unit>) -> Unit = { ms, _ -> slept += ms }

    private fun channel(sender: SmsSender, default: String = number) =
        SmsChannel(repo(default), sender, sleeper)

    @Test fun successFirstTry() = runBlocking {
        val sender = FakeSender(listOf(true))
        assertEquals(SmsSendResult.Sent(1), channel(sender).send(signed))
        assertEquals(listOf(number to expectedText), sender.sent)
        assertTrue(slept.isEmpty())
    }

    @Test fun retriesThenSucceeds() = runBlocking {
        val sender = FakeSender(listOf(false, false, true))
        assertEquals(SmsSendResult.Sent(3), channel(sender).send(signed))
        assertEquals(3, sender.sent.size)
        assertEquals(listOf(2_000L, 5_000L), slept)
    }

    @Test fun givesUpAfterThreeRetries() = runBlocking {
        val sender = FakeSender(emptyList())
        assertEquals(SmsSendResult.Failed(4), channel(sender).send(signed))
        assertEquals(4, sender.sent.size)
        assertEquals(listOf(2_000L, 5_000L, 10_000L), slept)
    }

    @Test fun blankNumberSendsNothing() = runBlocking {
        val sender = FakeSender(listOf(true))
        assertEquals(SmsSendResult.NoDestination, channel(sender, default = "  ").send(signed))
        assertTrue(sender.sent.isEmpty())
    }

    @Test fun missingPermissionSendsNothing() = runBlocking {
        val sender = FakeSender(listOf(true), permitted = false)
        assertEquals(SmsSendResult.NoPermission, channel(sender).send(signed))
        assertTrue(sender.sent.isEmpty())
    }

    @Test fun signedPayloadIsOneSms() = runBlocking {
        val sender = FakeSender(listOf(true))
        channel(sender).send(signed)
        val text = sender.sent.single().second
        assertEquals(137, text.length)
        assertTrue(text.length <= SmsChannel.MAX_SMS_CHARS)
    }

    @Test fun oversizedTextIsNotSent() = runBlocking {
        val sender = FakeSender(listOf(true))
        // 120 bytes -> "MAMA1 " + 160 Base64url characters = 166.
        assertEquals(SmsSendResult.TooLong(166), channel(sender).send(ByteArray(120)))
        assertTrue(sender.sent.isEmpty())
    }

    @Test fun stopDuringBackoffHaltsRetries() = runBlocking {
        val sender = FakeSender(emptyList())
        lateinit var channel: SmsChannel
        channel = SmsChannel(repo(number), sender) { ms, _ ->
            slept += ms
            channel.stop()
        }
        assertEquals(SmsSendResult.Stopped(1), channel.send(signed))
        assertEquals(1, sender.sent.size)
        assertEquals(listOf(2_000L), slept)
    }

    @Test fun stopCutsTheRealBackoffShort() = runBlocking {
        val sender = FakeSender(emptyList())
        val channel = SmsChannel(repo(number), sender)
        sender.onSend = { channel.stop() }
        val t0 = System.nanoTime()
        assertEquals(SmsSendResult.Stopped(1), channel.send(signed))
        assertTrue("stop must not wait for the 2s backoff", System.nanoTime() - t0 < 1_000_000_000L)
    }

    @Test fun cancellationHaltsRetries() = runBlocking {
        val sender = FakeSender(emptyList())
        val channel = SmsChannel(repo(number), sender) { _, _ -> throw CancellationException("cancelled") }
        try {
            channel.send(signed)
            error("send must propagate cancellation")
        } catch (e: CancellationException) {
            // expected
        }
        assertEquals(1, sender.sent.size)
    }

    @Test fun nextSendIsNotAffectedByAnEarlierStop() = runBlocking {
        val sender = FakeSender(listOf(true))
        val channel = channel(sender)
        channel.stop()
        assertEquals(SmsSendResult.Sent(1), channel.send(signed))
    }
}
