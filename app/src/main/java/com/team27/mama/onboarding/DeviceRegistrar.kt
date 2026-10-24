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

/**
 * Registers the device with its Ed25519 public key and the proof it holds the
 * private half (a signature over [DeviceRegistration.signedMessage]). Blocking.
 *
 * With the offline emergency flag on, setup (#15) calls it through
 * `setup.KeyedRegistration` (#59), with the [PayloadSigner] over :mesh's identity key.
 */
class DeviceRegistrar(
    private val api: ServerApi,
    private val signer: PayloadSigner,
) {
    fun register(registration: DeviceRegistration): RegisterResult {
        val b64 = Base64.getEncoder()
        val withKey = registration.copy(
            ed25519PublicKey = b64.encodeToString(signer.publicKey),
            ed25519Signature = b64.encodeToString(
                signer.sign(DeviceRegistration.signedMessage(registration.deviceId)),
            ),
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
