package com.team27.mama.emergency.payload

/** Kind of an offline emergency message. Code 4 is ack-only and is not a [Kind]. */
enum class Kind(val code: Int) {
    ALERT(1),
    UPDATE(2),
    CANCEL(3),
    ;

    companion object {
        fun fromCode(code: Int): Kind? = entries.firstOrNull { it.code == code }
    }
}

/**
 * Decoded 34-byte payload body. [deviceId] and [emergencyId] hold their 64 bits as a signed
 * Long (the high bit may be set). Every body rule is enforced at construction.
 */
data class EmergencyPayload(
    val kind: Kind,
    val deviceId: Long,
    val emergencyId: Long,
    val seq: Int,
    val latE5: Int,
    val lonE5: Int,
    val locAgeS: Int,
    val timestamp: Long,
) {
    init {
        require(seq in 0..65535) { "seq out of range: $seq" }
        require(latE5 in -PayloadFormat.LAT_E5_MAX..PayloadFormat.LAT_E5_MAX) { "lat out of range: $latE5" }
        require(lonE5 in -PayloadFormat.LON_E5_MAX..PayloadFormat.LON_E5_MAX) { "lon out of range: $lonE5" }
        require(locAgeS in 0..PayloadFormat.NO_LOCATION) { "loc_age_s out of range: $locAgeS" }
        require(locAgeS != PayloadFormat.NO_LOCATION || (latE5 == 0 && lonE5 == 0)) {
            "no-fix payload must have lat/lon 0"
        }
        require(timestamp in 0..4294967295L) { "timestamp out of range: $timestamp" }
    }
}

object PayloadFormat {
    const val VERSION = 1
    const val BODY_SIZE = 34
    const val SIGNATURE_SIZE = 64
    const val SIGNED_SIZE = 98
    const val KIND_ACK = 4
    const val ACK_BODY_SIZE = 12
    const val ACK_SIZE = 76
    const val NO_LOCATION = 0xFFFF
    const val MAX_LOC_AGE_S = 65534
    const val LAT_E5_MAX = 9_000_000
    const val LON_E5_MAX = 18_000_000
    const val SIGNATURE_CONTEXT = "MAMA-offline-emergency-v1"
}

/** The only exception decoders throw on hostile or corrupt input. */
class PayloadException(
    val reason: Reason,
    message: String,
    cause: Throwable? = null,
) : Exception(message, cause) {
    enum class Reason { FORMAT, SIGNATURE, DECRYPT }
}
