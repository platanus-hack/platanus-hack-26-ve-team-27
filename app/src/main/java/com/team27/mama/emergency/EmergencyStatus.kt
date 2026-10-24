package com.team27.mama.emergency

enum class DeliveryBadge { RUNNING, DONE, FAILED }

data class EmergencyStatus(val text: String, val detail: String?, val badge: DeliveryBadge)

/** What the trigger confirmation screen shows for this state; null while IDLE. */
fun EmergencyState.toStatus(): EmergencyStatus? {
    val (text, badge) = when (phase) {
        EmergencyPhase.IDLE -> return null
        EmergencyPhase.SENDING -> "Enviando…" to DeliveryBadge.RUNNING
        EmergencyPhase.SEARCHING_NEARBY -> "Sin conexión, buscando ayuda cercana" to DeliveryBadge.RUNNING
        EmergencyPhase.NOTIFIED -> "Ayuda notificada" to DeliveryBadge.DONE
        EmergencyPhase.NO_CHANNEL -> "Sin conexión. Activa Bluetooth o datos." to DeliveryBadge.FAILED
        EmergencyPhase.CANCELLING -> "Cancelando…" to DeliveryBadge.RUNNING
        EmergencyPhase.CANCELLED -> "Cancelado" to DeliveryBadge.DONE
    }
    val detail = if (phase == EmergencyPhase.SEARCHING_NEARBY || phase == EmergencyPhase.NO_CHANNEL) {
        when (meshIssue) {
            MeshIssue.NO_SERVER_KEYS -> "Para avisar sin internet, abre MAMÁ una vez con internet."
            MeshIssue.UNAVAILABLE -> "Activa Bluetooth para buscar ayuda cercana."
            null -> null
        }
    } else {
        null
    }
    return EmergencyStatus(text, detail, badge)
}
