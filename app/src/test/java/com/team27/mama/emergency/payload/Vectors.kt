package com.team27.mama.emergency.payload

import java.io.File
import kotlinx.serialization.SerialName
import kotlinx.serialization.Serializable
import kotlinx.serialization.json.Json

@Serializable
data class VectorKeys(
    @SerialName("device_ed25519_seed") val deviceEd25519Seed: String,
    @SerialName("device_ed25519_public") val deviceEd25519Public: String,
    @SerialName("device_id") val deviceId: String,
    @SerialName("server_ed25519_seed") val serverEd25519Seed: String,
    @SerialName("server_ed25519_public") val serverEd25519Public: String,
    @SerialName("server_x25519_private") val serverX25519Private: String,
    @SerialName("server_x25519_public") val serverX25519Public: String,
)

@Serializable
data class VectorDegrees(val deg: Double, val e5: Int)

@Serializable
data class VectorPayload(
    val name: String,
    val kind: Int,
    @SerialName("emergency_id") val emergencyId: String,
    val seq: Int,
    @SerialName("lat_e5") val latE5: Int,
    @SerialName("lon_e5") val lonE5: Int,
    @SerialName("loc_age_s") val locAgeS: Int,
    val timestamp: Long,
    val body: String,
    val signed: String,
    val sms: String,
    @SerialName("ephemeral_x25519_private") val ephemeralX25519Private: String,
    val sealed: String,
    val mesh: String,
)

@Serializable
data class VectorAck(
    val name: String,
    @SerialName("emergency_id") val emergencyId: String,
    val seq: Int,
    val ack: String,
    val sms: String,
    val mesh: String,
)

@Serializable
data class VectorInvalid(val name: String, val op: String, val input: String, val error: String)

@Serializable
data class VectorFile(
    val keys: VectorKeys,
    val degrees: List<VectorDegrees>,
    val payloads: List<VectorPayload>,
    val acks: List<VectorAck>,
    val invalid: List<VectorInvalid>,
)

object Vectors {
    val file: VectorFile by lazy {
        val candidates = listOf(
            File("../server/internal/emergency/payload/testdata/vectors.json"),
            File("server/internal/emergency/payload/testdata/vectors.json"),
        )
        val f = candidates.firstOrNull { it.isFile }
            ?: error(
                "payload vectors not found; tried " +
                    candidates.joinToString { it.absolutePath },
            )
        Json { ignoreUnknownKeys = false }.decodeFromString(VectorFile.serializer(), f.readText())
    }
}

fun ByteArray.hex(): String = joinToString("") { "%02x".format(it) }

fun String.unhex(): ByteArray {
    require(length % 2 == 0) { "odd hex length" }
    return ByteArray(length / 2) { substring(it * 2, it * 2 + 2).toInt(16).toByte() }
}
