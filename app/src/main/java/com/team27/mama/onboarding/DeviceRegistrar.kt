package com.team27.mama.onboarding

import com.team27.mama.contract.DeviceRegistration
import com.team27.mama.emergency.payload.PayloadSigner
import com.team27.mama.server.ServerApi
import com.team27.mama.server.ServerHttpException
import java.io.IOException
import java.util.Base64

sealed interface RegisterResult {
    data object Registered : RegisterResult
    data object KeyConflict : RegisterResult
    data class Failed(val cause: IOException) : RegisterResult
}

/** Registers the device with its Ed25519 public key attached. Blocking. */
class DeviceRegistrar(
    private val api: ServerApi,
    private val signer: PayloadSigner,
) {
    fun register(registration: DeviceRegistration): RegisterResult {
        val withKey = registration.copy(
            ed25519PublicKey = Base64.getEncoder().encodeToString(signer.publicKey),
        )
        return try {
            api.registerDevice(withKey)
            RegisterResult.Registered
        } catch (e: ServerHttpException) {
            if (e.code == 409) RegisterResult.KeyConflict else RegisterResult.Failed(e)
        } catch (e: IOException) {
            RegisterResult.Failed(e)
        }
    }
}
