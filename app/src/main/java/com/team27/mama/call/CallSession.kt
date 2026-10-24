package com.team27.mama.call

import kotlinx.coroutines.flow.StateFlow
import kotlin.time.Duration

/*
 * The in-app call with Mamá (#94), as the two call screens see it. The screens
 * only read [CallSession.state] and call answer() / hangUp(), so the engine
 * behind it can change without touching them: FakeCallSession plays the demo
 * script (and stays as the stage fallback), #97 plugs in the real ElevenLabs
 * agent.
 *
 * Bystander rule: a session carries what an ordinary call shows (who talks and
 * what was said), never a verdict or whether the family was told.
 */

enum class CallPhase { Ringing, Connected, Ended }

/** Who is talking right now. [None] is silence between turns. */
enum class Speaker { Mama, You, None }

/** One captions line. [partial] is the line still being spoken; it grows word by word. */
data class Caption(val speaker: Speaker, val text: String, val partial: Boolean)

data class CallState(
    val phase: CallPhase = CallPhase.Ringing,
    val elapsed: Duration = Duration.ZERO,
    val speaking: Speaker = Speaker.None,
    val captions: List<Caption> = emptyList(),
)

interface CallSession {
    val state: StateFlow<CallState>

    /** Picks up: Ringing -> Connected. Ignored in any other phase. */
    fun answer()

    /** Rejects or hangs up: -> Ended. Ignored once ended. */
    fun hangUp()
}
