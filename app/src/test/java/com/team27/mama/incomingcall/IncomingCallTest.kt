package com.team27.mama.incomingcall

import com.team27.mama.trigger.TriggerScreens
import com.team27.mama.ui.navigation.Route
import org.junit.Assert.assertEquals
import org.junit.Assert.assertFalse
import org.junit.Assert.assertNull
import org.junit.Assert.assertTrue
import org.junit.Test

private class FakeRinger : Ringer {
    val events = mutableListOf<String>()
    override fun ring(incidentId: String) {
        events += "ring $incidentId"
    }

    override fun stop() {
        events += "stop"
    }
}

class CallIntentTest {
    @Test
    fun everyActionIsItsOwnAppAction() {
        val actions = CallAction.entries.map { it.action }
        assertEquals(actions.size, actions.toSet().size)
        actions.forEach { assertTrue(it, it.startsWith("com.team27.mama.action.")) }
        assertEquals("com.team27.mama.action.INCOMING_CALL", CallAction.Ring.action)
    }

    @Test
    fun ringAndAnswerOpenAScreenWithTheIncidentId() {
        val ring = CallIntent(CallAction.Ring, "inc-1").extras()
        assertEquals(mapOf(TriggerScreens.EXTRA_INCIDENT_ID to "inc-1", TriggerScreens.EXTRA_ROUTE to RINGING_ROUTE.path), ring)
        val answer = CallIntent(CallAction.Answer, "inc-1").extras()
        assertEquals(mapOf(TriggerScreens.EXTRA_INCIDENT_ID to "inc-1", TriggerScreens.EXTRA_ROUTE to ANSWERED_ROUTE.path), answer)
    }

    @Test
    fun declineOpensNoScreen() {
        val decline = CallIntent(CallAction.Decline, "inc-1")
        assertNull(decline.route)
        assertEquals(mapOf(TriggerScreens.EXTRA_INCIDENT_ID to "inc-1"), decline.extras())
    }

    @Test
    fun theRoutesAreOnesMainActivityCanStartOn() {
        // MainActivity looks the "route" extra up in Route; an unknown path falls back to setup.
        listOf(RINGING_ROUTE, ANSWERED_ROUTE).forEach { route ->
            assertEquals(route, Route.entries.firstOrNull { it.path == route.path })
        }
    }

    @Test
    fun parseReadsBackWhatTheNotificationSends() {
        CallAction.entries.forEach { action ->
            val sent = CallIntent(action, "inc-7")
            val extras = sent.extras()
            assertEquals(sent, CallIntent.parse(action.action, extras[TriggerScreens.EXTRA_INCIDENT_ID]))
        }
    }

    @Test
    fun parseIgnoresOtherIntents() {
        assertNull(CallIntent.parse("android.intent.action.MAIN", "inc-1"))
        assertNull(CallIntent.parse(null, "inc-1"))
        assertNull(CallIntent.parse(CallAction.Ring.action, null))
        assertNull(CallIntent.parse(CallAction.Ring.action, " "))
    }

    @Test
    fun theCopyReadsAsAnOrdinaryCall() {
        val shown = listOf(CallCopy.CALLER, CallCopy.TEXT, CallCopy.CHANNEL).joinToString(" ").lowercase()
        listOf("alerta", "emergencia", "peligro", "auxilio", "ayuda", "sos", "pánico", "panico", "911").forEach {
            assertFalse("\"$it\" in \"$shown\"", Regex("\\b$it\\b").containsMatchIn(shown))
        }
        assertEquals("Mamá", CallCopy.CALLER)
        assertEquals("Llamada entrante", CallCopy.TEXT)
    }
}

class IncomingCallFlowTest {
    private val ringer = FakeRinger()
    private val cancelled = mutableListOf<String>()
    private val flow = IncomingCallFlow(ringer) {
        ringer.events += "cancel $it"
        cancelled += it
    }

    @Test
    fun declineStopsTheRingThenCancelsTheIncident() {
        flow.handle(CallIntent(CallAction.Decline, "inc-1"))
        assertEquals(listOf("stop", "cancel inc-1"), ringer.events)
        assertEquals(listOf("inc-1"), cancelled)
    }

    @Test
    fun answerStopsTheRingAndCancelsNothing() {
        flow.handle(CallIntent(CallAction.Answer, "inc-1"))
        assertEquals(listOf("stop"), ringer.events)
        assertTrue(cancelled.isEmpty())
    }

    @Test
    fun openingTheRingingScreenKeepsItRinging() {
        flow.handle(CallIntent(CallAction.Ring, "inc-1"))
        assertTrue(ringer.events.isEmpty())
    }
}

class CallScreenFollowerTest {
    private val ringer = FakeRinger()
    private val overLockScreen = mutableListOf<Boolean>()
    private val follower = CallScreenFollower(ringer) { overLockScreen += it }

    @Test
    fun leavingTheRingingScreenStopsTheRingOnce() {
        follower.onScreen(RINGING_ROUTE.path)
        assertTrue(ringer.events.isEmpty())
        follower.onScreen(Route.Setup.path)
        follower.onScreen(Route.Setup.path)
        assertEquals(listOf("stop"), ringer.events)
    }

    @Test
    fun otherScreensNeverStopARingTheyDidNotShow() {
        follower.onScreen(Route.Setup.path)
        follower.onScreen(null)
        assertTrue(ringer.events.isEmpty())
    }

    @Test
    fun onlyCallScreensShowOverTheLockScreen() {
        follower.onScreen(RINGING_ROUTE.path)
        follower.onScreen(ANSWERED_ROUTE.path)
        follower.onScreen(Route.Setup.path)
        assertEquals(listOf(true, true, false), overLockScreen)
    }
}
