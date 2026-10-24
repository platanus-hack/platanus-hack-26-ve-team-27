package com.team27.mama.relay

import kotlinx.coroutines.flow.StateFlow

/** What the settings screen needs from the relay: the opt-in switch and the counter. */
interface RelayControls {
    val enabled: StateFlow<Boolean>
    val relayedCount: StateFlow<Int>
    fun setEnabled(value: Boolean)
}

/** Delegates the switch to [RelaySettings] and the counter to [MeshRelay]. */
class DelegatingRelayControls(
    private val settings: RelaySettings,
    private val relay: MeshRelay,
) : RelayControls {
    override val enabled: StateFlow<Boolean> get() = settings.enabled
    override val relayedCount: StateFlow<Int> get() = relay.relayedCount
    override fun setEnabled(value: Boolean) = settings.setEnabled(value)
}

fun relayedCountText(n: Int): String = if (n == 1) "Reenviaste 1 alerta" else "Reenviaste $n alertas"
