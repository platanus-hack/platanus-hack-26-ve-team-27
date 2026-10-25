/*
 * The frozen JSON shapes shared by the phone, the server, the call webhook
 * and the notifier, plus the branch-by-speed rule. This file mirrors the Go
 * package server/internal/contract; docs/contract.md documents every shape
 * and server/internal/contract/testdata/ holds one canonical .json sample per
 * message, read by both the Go and the Kotlin round-trip tests.
 * Changing a shape after the contract freeze needs the Tech lead's OK.
 */
package com.team27.mama.contract

import kotlinx.serialization.SerialName
import kotlinx.serialization.Serializable
import kotlinx.serialization.json.Json
import kotlinx.serialization.json.JsonElement
import kotlinx.serialization.json.JsonPrimitive
import kotlinx.serialization.json.contentOrNull

/**
 * The one Json instance the app uses for contract shapes. Nulls are omitted
 * on encode (like Go `omitempty` on pointer fields), unknown fields are
 * ignored on decode (the ElevenLabs webhook carries many more), defaults are
 * encoded, and a JSON null for a non-null field with a default falls back to
 * that default.
 */
val ContractJson: Json = Json {
    ignoreUnknownKeys = true
    explicitNulls = false
    encodeDefaults = true
    coerceInputValues = true
}

/** The server's decision for an incident, picked from the speed. */
@Serializable
enum class Branch {
    /** Moving fast (in a car). The phone keeps sending points; no call. */
    @SerialName("silent_tracker") SILENT_TRACKER,

    /** Still. The family is alerted at once; no call. */
    @SerialName("immediate_alert") IMMEDIATE_ALERT,

    /** Otherwise, or speed unknown. "Mamá" calls. */
    @SerialName("call") CALL,
}

/**
 * Speed thresholds in meters per second (Android Location.getSpeed units).
 * Mirrors Go contract.StillBelowMPS and contract.FastFromMPS; change both
 * sides together.
 */
object SpeedThresholds {
    /** A speed strictly below this is still (< 3.6 km/h). */
    const val STILL_BELOW_MPS = 1.0

    /** A speed at or above this is fast (>= 25.2 km/h). */
    const val FAST_FROM_MPS = 7.0
}

/**
 * Picks the branch for a speed in m/s. A null speed (the fix had no speed)
 * or a negative or NaN one is unknown and gets [Branch.CALL], the branch
 * that asks the person.
 */
fun chooseBranch(speedMps: Double?): Branch = when {
    speedMps == null || !(speedMps >= 0) -> Branch.CALL
    speedMps < SpeedThresholds.STILL_BELOW_MPS -> Branch.IMMEDIATE_ALERT
    speedMps >= SpeedThresholds.FAST_FROM_MPS -> Branch.SILENT_TRACKER
    else -> Branch.CALL
}

/** How the person started the alert on the phone. */
@Serializable
enum class Trigger {
    /** Up-down-up, accessibility service. */
    @SerialName("volume_pattern") VOLUME_PATTERN,
    @SerialName("widget") WIDGET,

    /** Quick settings tile. */
    @SerialName("tile") TILE,
    @SerialName("in_app") IN_APP,
}

/** The duress detector's result for a call. */
@Serializable
enum class Verdict {
    @SerialName("duress") DURESS,
    @SerialName("no_duress") NO_DURESS,
    @SerialName("unclear") UNCLEAR,
}

/**
 * One GPS fix. [accuracyM] and [speedMps] are null when the fix has none.
 * [recordedAt] is RFC 3339 UTC; produce it with `java.time.Instant.now().toString()`.
 */
@Serializable
data class Point(
    val lat: Double,
    val lng: Double,
    @SerialName("accuracy_m") val accuracyM: Double? = null,
    @SerialName("speed_mps") val speedMps: Double? = null,
    @SerialName("recorded_at") val recordedAt: String,
)

/**
 * Phone -> server, sent at the end of first-run setup and again whenever the
 * setup changes. There is no auth: [deviceId] is a UUID the app generates at
 * install.
 */
@Serializable
data class DeviceRegistration(
    @SerialName("device_id") val deviceId: String,
    /** E.164; "Mamá" calls this number. */
    @SerialName("phone_number") val phoneNumber: String,
    /** E.164; the notifier writes to these. */
    @SerialName("family_numbers") val familyNumbers: List<String>,
    @SerialName("duress_phrase") val duressPhrase: String,
    @SerialName("app_version") val appVersion: String? = null,
    /**
     * How "Mamá" greets the person, 1 to 30 characters. Optional: null keeps
     * the name the server has stored.
     */
    @SerialName("user_name") val userName: String? = null,
    /**
     * The "todo bien" phrase the person says when they are safe. It must
     * differ from [duressPhrase] and not share most of its key words (the
     * server answers 422 otherwise). Optional: null keeps the stored phrase.
     */
    @SerialName("safe_phrase") val safePhrase: String? = null,
    /**
     * The device's Ed25519 identity public key (bitchat's, #56), std Base64
     * of 32 bytes; the server derives key_id from it to verify offline
     * payloads (#57).
     */
    @SerialName("ed25519_public_key") val ed25519PublicKey: String? = null,
    /**
     * Proof the phone holds [ed25519PublicKey]'s private half: std Base64 of
     * the 64-byte Ed25519 signature over [signedMessage] of [deviceId].
     * Required with [ed25519PublicKey] and only with it (the server answers
     * 400 otherwise), because bitchat broadcasts public keys on the mesh.
     */
    @SerialName("ed25519_signature") val ed25519Signature: String? = null,
) {
    companion object {
        /** Prefixes the bytes [ed25519Signature] covers; Go's RegistrationSignatureContext. */
        const val SIGNATURE_CONTEXT = "MAMA-device-registration-v1"

        /** The ASCII context, then the UTF-8 bytes of [deviceId]; Go's RegistrationSignedMessage. */
        fun signedMessage(deviceId: String): ByteArray =
            SIGNATURE_CONTEXT.toByteArray(Charsets.US_ASCII) + deviceId.toByteArray(Charsets.UTF_8)
    }
}

/**
 * Server -> phone, reply to GET /config; the phone caches it and works
 * offline from the cache. Keys are std Base64 of 32 bytes.
 */
@Serializable
data class RemoteConfig(
    /** E.164; omitted when the server has none. */
    @SerialName("sms_number") val smsNumber: String? = null,
    @SerialName("server_ed25519_public_key") val serverEd25519PublicKey: String,
    @SerialName("server_x25519_public_key") val serverX25519PublicKey: String,
)

/**
 * Phone -> server when the person triggers an alert. The phone generates
 * [incidentId] (a UUID), so a retried event is the same incident. [points]
 * holds at least one fix, oldest first; the last one's speed picks the
 * branch. [triggeredAt] is RFC 3339 UTC; produce it with
 * `java.time.Instant.now().toString()`. [deviceState] is the phone's
 * permissions at the gesture (#125); null leaves it out.
 */
@Serializable
data class AlertEvent(
    @SerialName("incident_id") val incidentId: String,
    @SerialName("device_id") val deviceId: String,
    val trigger: Trigger,
    @SerialName("triggered_at") val triggeredAt: String,
    val points: List<Point>,
    @SerialName("has_audio") val hasAudio: Boolean,
    @SerialName("device_state") val deviceState: DeviceState? = null,
) {
    /** The newest fix, or null when there is none. */
    fun lastPoint(): Point? = points.lastOrNull()

    /** Picks the branch from the newest fix's speed. */
    fun branch(): Branch = chooseBranch(lastPoint()?.speedMps)
}

/**
 * Permission names, the keys of [DeviceState.permissions] and the values of
 * [PermissionEvent.permission] (#125). Mirrors Go's contract.Permission*
 * names; change both sides together.
 */
object PermissionKeys {
    /** Precise location. */
    const val LOCATION_FINE = "location_fine"

    /** Location "all the time". */
    const val LOCATION_BACKGROUND = "location_background"
    const val MICROPHONE = "microphone"

    /** Read and write, to save "Mamá". */
    const val CONTACTS = "contacts"

    /** Send, for the offline emergency. */
    const val SMS = "sms"

    /** The app's notifications are on. */
    const val NOTIFICATIONS = "notifications"

    /** The volume gesture's accessibility service is on. */
    const val ACCESSIBILITY_SERVICE = "accessibility_service"

    /** Battery optimization is off for the app. */
    const val BATTERY_UNRESTRICTED = "battery_unrestricted"

    /** Android 14+ only. */
    const val FULL_SCREEN_INTENT = "full_screen_intent"

    /** Android 11+ only: the unused-app restrictions (hibernation) are off. */
    const val HIBERNATION_EXEMPT = "hibernation_exempt"

    /** Every name, in setup's order (#15): the order events at the same instant are sent in. */
    val ALL: List<String> = listOf(
        NOTIFICATIONS, LOCATION_FINE, LOCATION_BACKGROUND, MICROPHONE, CONTACTS, SMS,
        BATTERY_UNRESTRICTED, FULL_SCREEN_INTENT, HIBERNATION_EXEMPT, ACCESSIBILITY_SERVICE,
    )
}

/**
 * What the phone's permissions were when the person triggered the alert
 * (#125). [permissions] maps a [PermissionKeys] name to whether it was
 * granted; one that does not apply to the phone (its Android version has no
 * such thing) is left out. [capturedAt] is RFC 3339 UTC.
 */
@Serializable
data class DeviceState(
    @SerialName("captured_at") val capturedAt: String,
    val permissions: Map<String, Boolean>,
)

/**
 * Phone -> server, `POST /devices/{device_id}/permissions` (#125): when each
 * permission was granted or lost, as the phone saw it. The server ignores a
 * resent event.
 */
@Serializable
data class PermissionEvents(val events: List<PermissionEvent>)

/** One change of [permission] (a [PermissionKeys] name); [at] is RFC 3339 UTC, the phone clock. */
@Serializable
data class PermissionEvent(
    val permission: String,
    val granted: Boolean,
    val at: String,
)

/**
 * Server -> phone, the reply to an [AlertEvent]. [speedMps] is the speed the
 * branch was picked from, null when unknown.
 */
@Serializable
data class AlertResponse(
    @SerialName("incident_id") val incidentId: String,
    val branch: Branch,
    @SerialName("speed_mps") val speedMps: Double? = null,
)

/**
 * Phone -> server: more fixes for an open incident, sent while in silent
 * tracker mode (and by any branch that keeps tracking).
 */
@Serializable
data class LocationUpdate(
    @SerialName("incident_id") val incidentId: String,
    @SerialName("device_id") val deviceId: String,
    val points: List<Point>,
)

/**
 * Phone -> server: the person called off an open incident ("Fue sin querer"),
 * sent to POST /incidents/{incident_id}/cancel.
 */
@Serializable
data class IncidentCancel(
    @SerialName("incident_id") val incidentId: String,
    @SerialName("device_id") val deviceId: String,
)

/**
 * ElevenLabs -> server: the post-call webhook (type "post_call_transcription").
 * Only the fields the server reads are modeled; the real payload has more and
 * [ContractJson] ignores them.
 */
@Serializable
data class CallWebhook(
    val type: String,
    /** Unix seconds. */
    @SerialName("event_timestamp") val eventTimestamp: Long,
    val data: CallWebhookData,
) {
    /** The incident the call belongs to, or "" when missing or not a string. */
    fun incidentId(): String =
        (data.clientData.dynamicVariables[DYNAMIC_VAR_INCIDENT_ID] as? JsonPrimitive)
            ?.takeIf { it.isString }?.contentOrNull ?: ""
}

/** The "data" object of the post-call webhook. */
@Serializable
data class CallWebhookData(
    @SerialName("agent_id") val agentId: String,
    /** Unique per call; dedupes repeated webhooks. */
    @SerialName("conversation_id") val conversationId: String,
    val status: String,
    val transcript: List<TranscriptTurn>,
    val metadata: CallMetadata,
    /**
     * Carries the incident_id the server passed as a dynamic variable when it
     * placed the call.
     */
    @SerialName("conversation_initiation_client_data")
    val clientData: ConversationInitiationClientData,
)

/**
 * One turn of the call. [role] is "agent" ("Mamá") or "user". [message]
 * defaults to "" because ElevenLabs may send null.
 */
@Serializable
data class TranscriptTurn(
    val role: String,
    val message: String = "",
    @SerialName("time_in_call_secs") val timeInCallSecs: Double,
)

/** The part of the webhook metadata the server keeps. */
@Serializable
data class CallMetadata(
    @SerialName("start_time_unix_secs") val startTimeUnixSecs: Long,
    @SerialName("call_duration_secs") val callDurationSecs: Int,
)

/**
 * The dynamic variables of the call. Values are any JSON: ElevenLabs adds
 * system__* variables, some numeric.
 */
@Serializable
data class ConversationInitiationClientData(
    @SerialName("dynamic_variables") val dynamicVariables: Map<String, JsonElement> = emptyMap(),
)

/**
 * The dynamic variable that ties a call to its incident. The call engine sets
 * it; the webhook handler reads it back.
 */
const val DYNAMIC_VAR_INCIDENT_ID = "incident_id"

/** Dynamic variable: the person's [DeviceRegistration.userName], or "mi amor" when unset. */
const val DYNAMIC_VAR_NAME = "nombre"

/** Dynamic variable: the device's [DeviceRegistration.duressPhrase]. */
const val DYNAMIC_VAR_DURESS_PHRASE = "frase_alerta"

/** Dynamic variable: the device's [DeviceRegistration.safePhrase], or "" when unset. */
const val DYNAMIC_VAR_SAFE_PHRASE = "frase_segura"

/**
 * Server -> notifier: the one message the family gets for an incident. It
 * carries the location and the verdict, and never a link. [verdict] and
 * [reason] are null when no call happened (immediate_alert, silent_tracker).
 * [incidentId] is the dedupe key: one message per incident. [triggeredAt] is
 * RFC 3339 UTC; produce it with `java.time.Instant.now().toString()`.
 * [ficha] is the call's context (#106), null when the call has none.
 */
@Serializable
data class NotifierMessage(
    @SerialName("incident_id") val incidentId: String,
    /** E.164 family numbers. */
    val to: List<String>,
    val branch: Branch,
    val location: Point,
    val verdict: Verdict? = null,
    val reason: String? = null,
    @SerialName("triggered_at") val triggeredAt: String,
    val ficha: CallContext? = null,
)

/**
 * What an LLM took from a finished call's transcript (#106, `calls.ficha`).
 * Each field is "no se sabe" when the call did not say it. [confianza] is
 * "baja", "media" or "alta". Not the incident report's ficha.json (#76).
 */
@Serializable
data class CallContext(
    val lugar: String,
    val personas: String,
    val vehiculo: String,
    val rumbo: String,
    val situacion: String,
    val confianza: String,
)
