package com.team27.mama

import android.content.Context
import com.team27.mama.emergency.config.RemoteConfigRepository
import com.team27.mama.emergency.config.SharedPreferencesConfigStore
import com.team27.mama.emergency.payload.PayloadSigner
import com.team27.mama.onboarding.ConnectivityNetworkStatus
import com.team27.mama.onboarding.DeviceRegistrar
import com.team27.mama.onboarding.OnboardingGate
import com.team27.mama.server.HttpServerApi
import com.team27.mama.server.ServerApi

/**
 * Composition root. #59/#60 extend it with the mesh gateway, orchestrator and relay.
 *
 * Handoff: #15 builds the real DeviceRegistration and, after [OnboardingGate.finish]
 * returns Done, calls `deviceRegistrar(signer).register(...)`. #59 supplies the
 * [PayloadSigner] adapted from :mesh's MeshIdentity. #58/#59 read `remoteConfig.current()`.
 */
class AppGraph(context: Context) {
    private val app = context.applicationContext

    val serverApi: ServerApi = HttpServerApi(BuildConfig.SERVER_URL)

    val remoteConfig: RemoteConfigRepository =
        RemoteConfigRepository(serverApi, SharedPreferencesConfigStore(app), BuildConfig.DEFAULT_SMS_NUMBER)

    val onboardingGate: OnboardingGate = OnboardingGate(ConnectivityNetworkStatus(app), remoteConfig)

    fun deviceRegistrar(signer: PayloadSigner): DeviceRegistrar = DeviceRegistrar(serverApi, signer)
}
