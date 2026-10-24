package com.team27.mama.call

import androidx.lifecycle.ViewModel
import androidx.lifecycle.viewModelScope
import kotlinx.coroutines.CoroutineScope

/**
 * Keeps the current call alive across a rotation or a theme switch (#94): the
 * session runs on the ViewModel's scope, not on a composition that a config
 * change throws away. One call at a time; leaving the activity hangs it up.
 */
class CallViewModel : ViewModel() {
    private var session: CallSession? = null

    /** The call still going (ringing or connected), if any. */
    fun current(): CallSession? = session?.takeIf { it.state.value.phase != CallPhase.Ended }

    /** Starts a new call, hanging up whatever was there. */
    fun start(newSession: (CoroutineScope) -> CallSession): CallSession {
        session?.hangUp()
        return newSession(viewModelScope).also { session = it }
    }

    /** Hangs up and forgets the call. Safe to call twice. */
    fun end() {
        session?.hangUp()
        session = null
    }

    override fun onCleared() = end()
}
