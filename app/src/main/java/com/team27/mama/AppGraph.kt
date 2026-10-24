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
import com.team27.mama.emergency.config.RemoteConfigRepository
import com.team27.mama.emergency.config.SharedPreferencesConfigStore
import com.team27.mama.emergency.payload.PayloadSigner
import com.team27.mama.emergency.sms.AndroidSmsSender
import com.team27.mama.emergency.sms.ExecutorSmsScheduler
import com.team27.mama.emergency.sms.SmsAckBus
import com.team27.mama.emergency.sms.SmsAckFilter
import com.team27.mama.emergency.sms.SmsChannel
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
 * Handoff: nothing calls [deviceRegistrar] yet. #59 owns that call: it passes [deviceSigner]
 * and calls `deviceRegistrar(deviceSigner).register(...)` after [OnboardingGate.finish] returns
 * Done, with the real DeviceRegistration (phone, family numbers, duress phrase)
 * that #15's setup collects. #58/#59 read `remoteConfig.current()`.
 *
 * #59's orchestrator calls [smsChannel] when HTTP fails and there is cellular
 * signal. It and the ack plumbing are null with the flag off.
 *
 * [offlineEmergency] is the build flag OFFLINE_EMERGENCY_ENABLED (default off):
 * off, setup skips the config gate and the app never refreshes the config.
 */
@OptIn(ExperimentalCoroutinesApi::class)
class AppGraph(
    context: Context,
    val offlineEmergency: Boolean = BuildConfig.OFFLINE_EMERGENCY_ENABLED,
) {
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

    /** Verified server acks from [com.team27.mama.emergency.sms.SmsAckReceiver]. */
    val smsAcks: SmsAckBus? = if (offlineEmergency) SmsAckBus() else null

    val smsAckFilter: SmsAckFilter? = if (offlineEmergency) SmsAckFilter(remoteConfig::current) else null

    /** Built on first use so the flag-off app never starts its scheduler thread. */
    val smsChannel: SmsChannel? by lazy {
        val acks = smsAcks ?: return@lazy null
        val scheduler = ExecutorSmsScheduler(Executors.newSingleThreadScheduledExecutor())
        SmsChannel(AndroidSmsSender(app, scheduler), { remoteConfig.current().smsNumber }, scheduler, acks)
    }

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
            // Merge of main: #58's SmsChannel replaces the old `smsChannel` seam; adapted in the next commit.
            sms = NoSmsChannel,
            mesh = GatewayMeshChannel(gateway),
            location = AndroidLocationSource(app),
            network = networkStatus,
            channelOverride = channelOverride::value,
            scope = CoroutineScope(SupervisorJob() + Dispatchers.Default.limitedParallelism(1)),
            // Only the tag: a throwable's message or causes could carry a location or a number.
            onError = { message, _ -> Log.w(LOG_TAG, message) },
        )
    }

    fun deviceRegistrar(signer: PayloadSigner): DeviceRegistrar = DeviceRegistrar(serverApi, signer)

    private companion object {
        /** `adb logcat -s MamaEmergency` shows why an emergency fell back (tags and status codes only). */
        const val LOG_TAG = "MamaEmergency"
    }
}
