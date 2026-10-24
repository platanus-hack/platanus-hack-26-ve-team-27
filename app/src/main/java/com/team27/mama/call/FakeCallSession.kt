package com.team27.mama.call

import kotlinx.coroutines.CoroutineScope
import kotlinx.coroutines.Job
import kotlinx.coroutines.delay
import kotlinx.coroutines.flow.MutableStateFlow
import kotlinx.coroutines.flow.StateFlow
import kotlinx.coroutines.flow.asStateFlow
import kotlinx.coroutines.flow.update
import kotlinx.coroutines.isActive
import kotlinx.coroutines.launch
import kotlin.random.Random
import kotlin.time.Duration.Companion.seconds
import kotlin.time.TimeSource

/** One turn of the script: who says it, what, and the silence before it starts. */
data class ScriptLine(val speaker: Speaker, val text: String, val pauseBeforeMs: Long)

/**
 * The demo call, in real time. It is also the stage fallback when the network
 * fails (#97), so it has to sound like an ordinary call on its own. The duress
 * phrase is said like any other line: nothing marks it.
 */
val DemoScript: List<ScriptLine> = listOf(
    ScriptLine(Speaker.Mama, "¡Hola, mi amor! ¿Cómo vas? ¿Ya saliste del trabajo?", 900),
    ScriptLine(Speaker.You, "Sí, mami, ya voy saliendo.", 700),
    ScriptLine(Speaker.Mama, "¿Y a qué hora llegas? Te guardé comida.", 600),
    ScriptLine(Speaker.You, "Dile a la tía Carmen que llego tarde.", 900),
    ScriptLine(Speaker.Mama, "Ay, bueno, tranquilo, yo le digo. ¿Vienes en el carro?", 700),
    ScriptLine(Speaker.You, "No, me vine en camionetica.", 800),
    ScriptLine(Speaker.Mama, "Bueno, ve con cuidado. Avísame cuando estés llegando, ¿sí?", 600),
    ScriptLine(Speaker.You, "Dale, mami.", 700),
    ScriptLine(Speaker.Mama, "Te quiero mucho. Aquí te espero.", 500),
)

/** What the captions look like [atMs] after answering. */
data class ScriptFrame(val atMs: Long, val speaking: Speaker, val captions: List<Caption>)

// Mamá talks a little faster than someone answering on the street.
private fun msPerWord(speaker: Speaker): Long = if (speaker == Speaker.Mama) 320 else 380

/**
 * The script as a timeline: one frame per spoken word (the line grows as a
 * partial caption), then a frame that closes the line and goes silent.
 */
fun scriptFrames(script: List<ScriptLine>): List<ScriptFrame> {
    val frames = mutableListOf<ScriptFrame>()
    val done = mutableListOf<Caption>()
    var at = 0L
    for (line in script) {
        at += line.pauseBeforeMs
        val words = line.text.split(' ').filter { it.isNotEmpty() }
        for (count in 1..words.size) {
            val partial = Caption(line.speaker, words.take(count).joinToString(" "), partial = true)
            frames += ScriptFrame(at, line.speaker, done + partial)
            at += msPerWord(line.speaker)
        }
        done += Caption(line.speaker, line.text, partial = false)
        frames += ScriptFrame(at, Speaker.None, done.toList())
    }
    return frames
}

/**
 * Plays [script] on [scope] once answered: captions word by word, the clock
 * every second and a made-up voice level while someone talks. The call stays
 * connected after the last line until hangUp().
 */
class FakeCallSession(
    private val scope: CoroutineScope,
    private val script: List<ScriptLine> = DemoScript,
) : CallSession {
    private val _state = MutableStateFlow(CallState())
    override val state: StateFlow<CallState> = _state.asStateFlow()

    private val jobs = mutableListOf<Job>()

    override fun answer() {
        if (_state.value.phase != CallPhase.Ringing) return
        _state.update { it.copy(phase = CallPhase.Connected) }
        val start = TimeSource.Monotonic.markNow()

        jobs += scope.launch {
            while (isActive) {
                delay(1_000)
                _state.update { it.copy(elapsed = start.elapsedNow().inWholeSeconds.seconds) }
            }
        }
        jobs += scope.launch {
            for (frame in scriptFrames(script)) {
                val wait = frame.atMs - start.elapsedNow().inWholeMilliseconds
                if (wait > 0) delay(wait)
                _state.update { it.copy(speaking = frame.speaking, captions = frame.captions) }
            }
        }
        jobs += scope.launch {
            while (isActive) {
                delay(90)
                _state.update { it.copy(level = if (it.speaking == Speaker.None) 0f else Random.nextFloat() * 0.6f + 0.4f) }
            }
        }
    }

    override fun hangUp() {
        if (_state.value.phase == CallPhase.Ended) return
        jobs.forEach { it.cancel() }
        jobs.clear()
        _state.update { it.copy(phase = CallPhase.Ended, speaking = Speaker.None, level = 0f) }
    }
}
