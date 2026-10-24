package com.team27.mama

import android.content.Context
import android.util.Log
import com.team27.mama.emergency.AndroidLocationSource
import com.team27.mama.emergency.ChannelOverrideSetting
import com.team27.mama.emergency.EmergencyOrchestrator
import com.team27.mama.emergency.GatewayMeshChannel
import com.team27.mama.emergency.MeshIdentitySigner
import com.team27.mama.emergency.NoSmsChannel
import com.team27.mama.emergency.ServerEmergencyChannel
import com.team27.mama.emergency.SmsEmergencyChannel
import com.team27.mama.emergency.config.RemoteConfigRepository
import com.team27.mama.emergency.config.SharedPreferencesConfigStore
import com.team27.mama.emergency.payload.PayloadSigner
import com.team27.mama.mesh.MeshGateway
import com.team27.mama.onboarding.ConnectivityNetworkStatus
import com.team27.mama.onboarding.DeviceRegistrar
import com.team27.mama.onboarding.OnboardingGate
import com.team27.mama.server.HttpServerApi
import com.team27.mama.server.ServerApi
import com.team27.mama.server.UrlConnectionTransport
import java.util.concurrent.Executor
import java.util.concurrent.Executors
import kotlinx.coroutines.CoroutineScope
import kotlinx.coroutines.Dispatchers
import kotlinx.coroutines.ExperimentalCoroutinesApi
import kotlinx.coroutines.SupervisorJob

/**
 * Composition root. #60 extends it with the relay.
 *
 * Handoff: nothing calls [deviceRegistrar] yet. #15 owns that call: it passes [deviceSigner]
 * and calls `deviceRegistrar(deviceSigner).register(...)` after [OnboardingGate.finish] returns
 * Done, with the real DeviceRegistration (phone, family numbers, duress phrase)
 * that its setup collects. #58 reads `remoteConfig.current()`.
 */
@OptIn(ExperimentalCoroutinesApi::class)
class AppGraph(context: Context) {
    private val app = context.applicationContext

    /** One background thread for config refreshes (network edges and onboarding). */
    val background: Executor = Executors.newSingleThreadExecutor()

    val serverApi: ServerApi = HttpServerApi(BuildConfig.SERVER_URL)

    val remoteConfig: RemoteConfigRepository =
        RemoteConfigRepository(serverApi, SharedPreferencesConfigStore(app), BuildConfig.DEFAULT_SMS_NUMBER)

    private val networkStatus = ConnectivityNetworkStatus(app)

    val onboardingGate: OnboardingGate = OnboardingGate(networkStatus, remoteConfig, background)

    /** #61's hidden debug toggle sets `graph.channelOverride.value`. */
    val channelOverride: ChannelOverrideSetting = ChannelOverrideSetting()

    /**
     * The device key (the bitchat identity). #15 passes it to [deviceRegistrar] so the registered key
     * is the one that signs. Building it never throws: a broken key store surfaces on use.
     */
    val deviceSigner: PayloadSigner by lazy { MeshIdentitySigner.deferred(MeshGateway.getInstance(app)) }

    /** #58 replaces this with the real sender before the first `declare()`. */
    var smsChannel: SmsEmergencyChannel = NoSmsChannel

    /** #60 reads `graph.emergency.state`; #16 calls `declare()`. */
    val emergency: EmergencyOrchestrator by lazy {
        val gateway = MeshGateway.getInstance(app)
        EmergencyOrchestrator(
            signer = deviceSigner,
            config = remoteConfig::current,
            http = ServerEmergencyChannel(
                BuildConfig.SERVER_URL,
                UrlConnectionTransport(5_000, 5_000),
                log = { Log.w(LOG_TAG, it) },
            ),
            sms = smsChannel,
            mesh = GatewayMeshChannel(gateway),
            location = AndroidLocationSource(app),
            network = networkStatus,
            channelOverride = channelOverride::value,
            scope = CoroutineScope(SupervisorJob() + Dispatchers.Default.limitedParallelism(1)),
            onError = { message, error -> Log.w(LOG_TAG, message, error) },
        )
    }

    fun deviceRegistrar(signer: PayloadSigner): DeviceRegistrar = DeviceRegistrar(serverApi, signer)

    private companion object {
        /** `adb logcat -s MamaEmergency` shows why an emergency fell back (tags and status codes only). */
        const val LOG_TAG = "MamaEmergency"
    }
}
