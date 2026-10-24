package com.team27.mama.emergency

import org.junit.Assert.assertEquals
import org.junit.Assert.assertNull
import org.junit.Test

class EmergencyStatusTest {
    private fun state(phase: EmergencyPhase, issue: MeshIssue? = null) =
        EmergencyState(phase = phase, meshIssue = issue)

    @Test
    fun statusTexts() {
        assertNull(state(EmergencyPhase.IDLE).toStatus())
        assertEquals(
            EmergencyStatus("Enviando…", null, DeliveryBadge.RUNNING),
            state(EmergencyPhase.SENDING).toStatus(),
        )
        assertEquals(
            EmergencyStatus("Sin conexión, buscando ayuda cercana", null, DeliveryBadge.RUNNING),
            state(EmergencyPhase.SEARCHING_NEARBY).toStatus(),
        )
        assertEquals(
            EmergencyStatus("Ayuda notificada", null, DeliveryBadge.DONE),
            state(EmergencyPhase.NOTIFIED).toStatus(),
        )
        assertEquals(
            EmergencyStatus("Sin conexión. Activa Bluetooth o datos.", null, DeliveryBadge.FAILED),
            state(EmergencyPhase.NO_CHANNEL).toStatus(),
        )
        assertEquals(
            EmergencyStatus("Cancelando…", null, DeliveryBadge.RUNNING),
            state(EmergencyPhase.CANCELLING).toStatus(),
        )
        assertEquals(
            EmergencyStatus("Cancelado", null, DeliveryBadge.DONE),
            state(EmergencyPhase.CANCELLED).toStatus(),
        )
        assertEquals(
            "Activa Bluetooth para buscar ayuda cercana.",
            state(EmergencyPhase.SEARCHING_NEARBY, MeshIssue.UNAVAILABLE).toStatus()?.detail,
        )
        assertEquals(
            "Para avisar sin internet, abre MAMÁ una vez con internet.",
            state(EmergencyPhase.NO_CHANNEL, MeshIssue.NO_SERVER_KEYS).toStatus()?.detail,
        )
        assertNull(state(EmergencyPhase.NOTIFIED, MeshIssue.UNAVAILABLE).toStatus()?.detail)
    }
}
