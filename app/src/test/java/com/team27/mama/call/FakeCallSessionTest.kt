package com.team27.mama.call

import com.team27.mama.ui.screens.formatElapsed
import kotlinx.coroutines.CoroutineScope
import kotlinx.coroutines.Job
import kotlinx.coroutines.cancel
import org.junit.Assert.assertEquals
import org.junit.Assert.assertFalse
import org.junit.Assert.assertTrue
import org.junit.Test
import kotlin.time.Duration.Companion.minutes
import kotlin.time.Duration.Companion.seconds

class FakeCallSessionTest {
    private val script = listOf(
        ScriptLine(Speaker.Mama, "Hola mi amor", 500),
        ScriptLine(Speaker.You, "Ya voy", 300),
    )

    @Test
    fun partialLineGrowsWordByWordThenCloses() {
        val frames = scriptFrames(script)
        val mama = frames.take(4).map { it.captions.last() }
        assertEquals(
            listOf("Hola", "Hola mi", "Hola mi amor", "Hola mi amor"),
            mama.map { it.text },
        )
        assertEquals(listOf(true, true, true, false), mama.map { it.partial })
        assertEquals(Speaker.Mama, frames[0].speaking)
        assertEquals(Speaker.None, frames[3].speaking)
    }

    @Test
    fun framesAreInTimeOrderAndStartAfterThePause() {
        val frames = scriptFrames(script)
        assertEquals(500L, frames.first().atMs)
        assertTrue(frames.zipWithNext().all { (a, b) -> a.atMs <= b.atMs })
    }

    @Test
    fun scriptEndsWithEveryLineClosedAndInOrder() {
        val last = scriptFrames(DemoScript).last()
        assertEquals(DemoScript.map { it.text }, last.captions.map { it.text })
        assertTrue(last.captions.none { it.partial })
        assertEquals(Speaker.None, last.speaking)
    }

    @Test
    fun demoScriptHasNoAlarmWords() {
        val alarm = Regex("alerta|emergencia|peligro|auxilio|ayuda|socorro", RegexOption.IGNORE_CASE)
        assertTrue(DemoScript.none { alarm.containsMatchIn(it.text) })
    }

    @Test
    fun answerThenHangUpEndsAndStaysEnded() {
        val scope = CoroutineScope(Job())
        try {
            val session = FakeCallSession(scope, script)
            assertEquals(CallPhase.Ringing, session.state.value.phase)
            session.answer()
            assertEquals(CallPhase.Connected, session.state.value.phase)
            session.hangUp()
            assertEquals(CallPhase.Ended, session.state.value.phase)
            assertEquals(Speaker.None, session.state.value.speaking)
            session.answer()
            assertEquals(CallPhase.Ended, session.state.value.phase)
        } finally {
            scope.cancel()
        }
    }

    @Test
    fun rejectWhileRingingEndsWithoutCaptions() {
        val scope = CoroutineScope(Job())
        try {
            val session = FakeCallSession(scope, script)
            session.hangUp()
            assertEquals(CallPhase.Ended, session.state.value.phase)
            assertFalse(session.state.value.captions.isNotEmpty())
        } finally {
            scope.cancel()
        }
    }

    @Test
    fun viewModelKeepsOneCallAndForgetsItOnEnd() {
        val calls = CallViewModel()
        val first = calls.start { FakeCallSession(it, script) }
        assertEquals(first, calls.current())
        val second = calls.start { FakeCallSession(it, script) }
        assertEquals(CallPhase.Ended, first.state.value.phase)
        assertEquals(second, calls.current())
        calls.end()
        assertEquals(null, calls.current())
        assertEquals(CallPhase.Ended, second.state.value.phase)
    }

    @Test
    fun elapsedReadsLikeAPhoneTimer() {
        assertEquals("00:00", formatElapsed(0.seconds))
        assertEquals("00:42", formatElapsed(42.seconds))
        assertEquals("75:05", formatElapsed(75.minutes + 5.seconds))
    }
}
