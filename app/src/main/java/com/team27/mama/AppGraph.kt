package com.team27.mama

import android.Manifest
import android.content.Context
import android.content.pm.PackageManager
import android.util.Log
import androidx.core.content.ContextCompat
import com.team27.mama.emergency.AndroidLocationSource
import com.team27.mama.emergency.ChannelOverrideSetting
import com.team27.mama.emergency.EmergencyOrchestrator
import com.team27.mama.emergency.GatewayMeshChannel
import com.team27.mama.emergency.MeshIdentitySigner
import com.team27.mama.emergency.ServerEmergencyChannel
import com.team27.mama.emergency.config.RemoteConfigRepository
import com.team27.mama.emergency.config.SharedPreferencesConfigStore
import com.team27.mama.emergency.payload.PayloadSigner
import com.team27.mama.emergency.sms.AndroidSmsSender
import com.team27.mama.emergency.sms.ExecutorSmsScheduler
import com.team27.mama.emergency.sms.SmsAckBus
import com.team27.mama.emergency.sms.SmsAckFilter
import com.team27.mama.emergency.sms.SmsChannel
import com.team27.mama.emergency.sms.SmsChannelAdapter
import com.team27.mama.mesh.MeshGateway
import com.team27.mama.onboarding.ConnectivityNetworkStatus
import com.team27.mama.onboarding.DeviceRegistrar
import com.team27.mama.onboarding.OnboardingGate
import com.team27.mama.relay.GatewayRelayMesh
import com.team27.mama.relay.MeshRelay
import com.team27.mama.relay.DelegatingRelayControls
import com.team27.mama.relay.OwnTextMeshChannel
import com.team27.mama.relay.RelayControls
import com.team27.mama.relay.RelaySettings
import com.team27.mama.relay.ServerRelayUploader
import com.team27.mama.relay.SharedPreferencesRelayStore
import com.team27.mama.server.HttpServerApi
import com.team27.mama.server.ServerApi
import com.team27.mama.server.UrlConnectionTransport
import com.team27.mama.trigger.PhoneCallOnlyLauncher
import com.team27.mama.trigger.IncomingCallLauncher
import java.util.concurrent.Executor
import java.util.concurrent.Executors
import kotlinx.coroutines.CoroutineScope
import kotlinx.coroutines.Dispatchers
import kotlinx.coroutines.ExperimentalCoroutinesApi
import kotlinx.coroutines.SupervisorJob

/**
 * Composition root. #60 extends it with the relay.
 *
 * With the flag on, setup (#15) registers through `deviceRegistrar(deviceSigner)` (#59's
 * `setup.KeyedRegistration`), so the registered key is the one that signs emergencies.
 * #58/#59 read `remoteConfig.current()`.
 *
 * #59's orchestrator reaches [smsChannel] through `SmsChannelAdapter` when HTTP fails;
 * the adapter also forwards the verified ack bodies from [smsAcks]. Both are null with
 * the flag off.
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

    internal val networkStatus = ConnectivityNetworkStatus(app)

    val onboardingGate: OnboardingGate = OnboardingGate(networkStatus, remoteConfig, background)

    /** #61's hidden debug toggle sets `graph.channelOverride.value`. */
    val channelOverride: ChannelOverrideSetting = ChannelOverrideSetting()

    /**
     * The device key (the bitchat identity). Setup passes it to [deviceRegistrar] so the registered
     * key is the one that signs. Building it never throws: a broken key store surfaces on use.
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

    /**
     * #60 reads `graph.emergency?.state`. The trigger service calls `declare()` when its alert
     * fails (`emergency.OfflineFallback`). Null with the flag off: the orchestrator and the mesh
     * gateway never start. With the flag on it is built eagerly in `Application.onCreate`,
     * because MamaApp reads its state.
     */
    val emergency: EmergencyOrchestrator? by lazy {
        if (!offlineEmergency) return@lazy null
        val gateway = MeshGateway.getInstance(app)
        EmergencyOrchestrator(
            signer = deviceSigner,
            config = remoteConfig::current,
            http = ServerEmergencyChannel(
                BuildConfig.SERVER_URL,
                UrlConnectionTransport(5_000, 5_000),
                log = { Log.w(LOG_TAG, it) },
            ),
            sms = SmsChannelAdapter(
                smsChannel,
                smsAcks,
                centralNumber = { remoteConfig.current().smsNumber },
                sendPermitted = {
                    ContextCompat.checkSelfPermission(app, Manifest.permission.SEND_SMS) == PackageManager.PERMISSION_GRANTED
                },
            ),
            mesh = OwnTextMeshChannel(GatewayMeshChannel(gateway), requireNotNull(relay)::markOwn),
            location = AndroidLocationSource(app),
            network = networkStatus,
            channelOverride = channelOverride::value,
            scope = CoroutineScope(SupervisorJob() + Dispatchers.Default.limitedParallelism(1)),
            // Only the tag: a throwable's message or causes could carry a location or a number.
            onError = { message, _ -> Log.w(LOG_TAG, message) },
        )
    }

    /** Opt-in switch for the relay (MeshController, #60's settings screen). Null with the flag off. */
    val relaySettings: RelaySettings? by lazy { if (offlineEmergency) RelaySettings(app) else null }

    /**
     * The BLE relay (#60): MamaApp starts it, the settings screen reads its counters, and the
     * orchestrator's mesh channel feeds it [MeshRelay.markOwn]. Null with the flag off. Its scope
     * is single-threaded because the relay confines its snapshot to it.
     */
    val relay: MeshRelay? by lazy {
        if (!offlineEmergency) return@lazy null
        MeshRelay(
            GatewayRelayMesh(MeshGateway.getInstance(app)),
            ServerRelayUploader(BuildConfig.SERVER_URL, UrlConnectionTransport(10_000, 10_000)),
            networkStatus,
            SharedPreferencesRelayStore(app),
            CoroutineScope(SupervisorJob() + Dispatchers.Default.limitedParallelism(1)),
            // Info: every line is a routine outcome (queued, acked, a retried failure). The real failures, a
            // refused service start or mesh call, are warnings from MeshController and RelayForegroundService.
            log = { Log.i("MamaRelay", it) },
        )
    }

    /** The settings screen's view of the relay. Null with the flag off, so no Settings route exists. */
    val relayControls: RelayControls? by lazy {
        val settings = relaySettings ?: return@lazy null
        val mesh = relay ?: return@lazy null
        DelegatingRelayControls(settings, mesh)
    }

    fun deviceRegistrar(signer: PayloadSigner): DeviceRegistrar = DeviceRegistrar(serverApi, signer)

    /**
     * What the trigger (#16) runs when the server answers an alert with branch
     * `call`. The demo answers "Mamá" as a normal phone call (Twilio, #18), so
     * the app opens nothing. The in-app call (#94, #96) is out of the demo.
     */
    val incomingCall: IncomingCallLauncher = PhoneCallOnlyLauncher(app)

    private companion object {
        /** `adb logcat -s MamaEmergency` shows why an emergency fell back (tags and status codes only). */
        const val LOG_TAG = "MamaEmergency"
    }
}
