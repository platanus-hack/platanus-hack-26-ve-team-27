package com.team27.mama.setup

import com.team27.mama.contract.DeviceRegistration
import com.team27.mama.emergency.MeshUnavailableException
import com.team27.mama.onboarding.DeviceRegistrar
import com.team27.mama.onboarding.RegisterResult
import com.team27.mama.server.ServerHttpException
import kotlinx.coroutines.CoroutineDispatcher
import kotlinx.coroutines.Dispatchers
import kotlinx.coroutines.withContext

/**
 * `POST /devices` with the device key (#59), used by setup instead of [RegistrationClient]
 * when the offline emergency flag is on: [DeviceRegistrar] adds the Ed25519 public key and the
 * proof-of-possession signature the server needs to verify offline emergencies. Failures use
 * [RegistrationClient]'s Spanish messages.
 */
object KeyedRegistration {
    const val KEY_UNAVAILABLE = "No pudimos preparar la llave de este teléfono. Inténtalo de nuevo."

    /** [DeviceRegistrar] blocks, so it runs on [io]. */
    suspend fun register(
        registrar: DeviceRegistrar,
        registration: DeviceRegistration,
        io: CoroutineDispatcher = Dispatchers.IO,
    ): RegistrationResult = withContext(io) {
        try {
            when (val result = registrar.register(registration)) {
                RegisterResult.Registered -> RegistrationResult.Registered
                RegisterResult.KeyConflict -> RegistrationClient.resultFor(409)
                is RegisterResult.Failed -> (result.cause as? ServerHttpException)
                    ?.let { RegistrationClient.resultFor(it.code, it.body) }
                    ?: RegistrationResult.Failed(RegistrationClient.NETWORK_ERROR)
            }
        } catch (_: MeshUnavailableException) {
            // The mesh key store is unusable, so there is no key to register.
            RegistrationResult.Failed(KEY_UNAVAILABLE)
        }
    }
}
