package com.team27.mama.trigger

import com.team27.mama.contract.AlertEvent
import com.team27.mama.contract.AlertResponse
import com.team27.mama.contract.Branch
import com.team27.mama.contract.ContractJson
import com.team27.mama.contract.Trigger
import kotlinx.coroutines.runBlocking
import org.junit.Assert.assertEquals
import org.junit.Assert.assertNull
import org.junit.Assert.assertTrue
import org.junit.Test

class AlertSenderTest {

    private val event = AlertEvents.alertEvent(
        incidentId = "9b2e4f1d-3c5a-4e7b-8d6f-1a2b3c4d5e6f",
        deviceId = "3f1c9a52-7b1e-4c8a-9a0e-2d6b8f4e1a77",
        trigger = Trigger.VOLUME_PATTERN,
        triggeredAtMs = 1_761_296_400_253,
        points = listOf(AlertEvents.point(10.4961, -66.8531, 12.5f, null, 1_761_296_400_000)),
    )

    private fun response(branch: Branch) =
        ContractJson.encodeToString(AlertResponse.serializer(), AlertResponse(event.incidentId, branch))

    /** Answers with [answers] in order and records every body it was given. */
    private class FakeServer(vararg answers: HttpResult) {
        private val queue = ArrayDeque(answers.toList())
        val bodies = mutableListOf<String>()
        var registrations = 0
        var registrationWorks = true
        val sleeps = mutableListOf<Long>()

        suspend fun post(body: String): HttpResult {
            bodies += body
            return queue.removeFirst()
        }

        suspend fun reregister(): Boolean {
            registrations++
            return registrationWorks
        }
    }

    private fun sender(server: FakeServer, delays: List<Long> = listOf(1_000, 2_000, 4_000)) =
        AlertSender(server::post, server::reregister, delays, sleep = { server.sleeps += it })

    @Test
    fun aFirstAnswerIsAcceptedWithItsBranch() = runBlocking {
        val server = FakeServer(HttpResult(201, response(Branch.CALL)))
        val outcome = sender(server).send(event)
        assertEquals(AlertOutcome.Accepted(AlertResponse(event.incidentId, Branch.CALL), attempts = 1), outcome)
        assertEquals(emptyList<Long>(), server.sleeps)
    }

    @Test
    fun everyRetrySendsTheSameBodyWithTheSameIncidentId() = runBlocking {
        val server = FakeServer(HttpResult(-1), HttpResult(503), HttpResult(200, response(Branch.IMMEDIATE_ALERT)))
        val outcome = sender(server).send(event)
        assertEquals(3, outcome.attempts)
        assertTrue(outcome is AlertOutcome.Accepted)
        assertEquals(1, server.bodies.toSet().size)
        val ids = server.bodies.map { ContractJson.decodeFromString(AlertEvent.serializer(), it).incidentId }
        assertEquals(List(3) { event.incidentId }, ids)
        // It waits between tries: the first delays of the list, in order.
        assertEquals(listOf(1_000L, 2_000L), server.sleeps)
    }

    @Test
    fun aRetriedIncidentAnswering200KeepsTheStoredBranch() = runBlocking {
        // The first POST reached the server but the answer was lost; the retry gets 200 + the stored decision.
        val server = FakeServer(HttpResult(-1), HttpResult(200, response(Branch.SILENT_TRACKER)))
        val outcome = sender(server).send(event) as AlertOutcome.Accepted
        assertEquals(Branch.SILENT_TRACKER, outcome.response?.branch)
    }

    @Test
    fun anUnknownDeviceReRegistersThenResendsAtOnce() = runBlocking {
        val server = FakeServer(HttpResult(422), HttpResult(201, response(Branch.CALL)))
        val outcome = sender(server).send(event)
        assertEquals(1, server.registrations)
        assertEquals(2, outcome.attempts)
        assertTrue(outcome is AlertOutcome.Accepted)
        assertEquals(emptyList<Long>(), server.sleeps)
        assertEquals(server.bodies[0], server.bodies[1])
    }

    @Test
    fun aFailedReRegistrationWaitsAndTriesAgain() = runBlocking {
        val server = FakeServer(HttpResult(422), HttpResult(422), HttpResult(201, response(Branch.CALL))).apply { registrationWorks = false }
        val outcome = sender(server).send(event)
        assertTrue(outcome is AlertOutcome.Accepted)
        assertEquals(2, server.registrations)
        assertEquals(listOf(1_000L, 2_000L), server.sleeps)
    }

    @Test
    fun a422ThatOutlivesTheRegistrationsStops() = runBlocking {
        val server = FakeServer(HttpResult(422), HttpResult(422), HttpResult(422))
        assertEquals(AlertOutcome.Rejected(422, attempts = 3), sender(server).send(event))
        assertEquals(2, server.registrations)
    }

    @Test
    fun aBadRequestIsNotRetried() = runBlocking {
        val server = FakeServer(HttpResult(400, """{"error":"points needs at least one fix"}"""))
        assertEquals(AlertOutcome.Rejected(400, attempts = 1), sender(server).send(event))
        assertEquals(emptyList<Long>(), server.sleeps)
    }

    @Test
    fun itGivesUpWhenTheDelaysRunOut() = runBlocking {
        val server = FakeServer(HttpResult(-1), HttpResult(-1), HttpResult(500))
        assertEquals(AlertOutcome.GaveUp(500, attempts = 3), sender(server, delays = listOf(10, 20)).send(event))
        assertEquals(listOf(10L, 20L), server.sleeps)
    }

    @Test
    fun anUnreadable2xxIsStillAccepted() = runBlocking {
        val outcome = sender(FakeServer(HttpResult(201, "not json"))).send(event)
        assertEquals(AlertOutcome.Accepted(null, attempts = 1), outcome)
    }

    @Test
    fun onlyClientErrorsThatCannotChangeArePermanent() {
        assertTrue(AlertSender.isPermanent(400))
        assertTrue(AlertSender.isPermanent(409))
        assertEquals(false, AlertSender.isPermanent(408))
        assertEquals(false, AlertSender.isPermanent(429))
        assertEquals(false, AlertSender.isPermanent(503))
        assertEquals(false, AlertSender.isPermanent(-1))
        assertNull(AlertSender.parseResponse(""))
    }
}
