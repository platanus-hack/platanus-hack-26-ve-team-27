package com.team27.mama

import android.content.Context
import com.team27.mama.emergency.config.RemoteConfigRepository
import com.team27.mama.emergency.config.SharedPreferencesConfigStore
import com.team27.mama.emergency.payload.PayloadSigner
import com.team27.mama.emergency.sms.AndroidSmsSender
import com.team27.mama.emergency.sms.ExecutorSmsScheduler
import com.team27.mama.emergency.sms.SmsAckBus
import com.team27.mama.emergency.sms.SmsAckFilter
import com.team27.mama.emergency.sms.SmsChannel
import com.team27.mama.onboarding.ConnectivityNetworkStatus
import com.team27.mama.onboarding.DeviceRegistrar
import com.team27.mama.onboarding.OnboardingGate
import com.team27.mama.server.HttpServerApi
import com.team27.mama.server.ServerApi
import com.team27.mama.trigger.ConfirmationScreenLauncher
import com.team27.mama.trigger.IncomingCallLauncher
import java.util.concurrent.Executor
import java.util.concurrent.Executors

/**
 * Composition root. #59/#60 extend it with the mesh gateway, orchestrator and relay.
 *
 * Handoff: nothing calls [deviceRegistrar] in #55. #59 owns that call: it adapts
 * :mesh's MeshIdentity into the [PayloadSigner] and calls
 * `deviceRegistrar(signer).register(...)` after [OnboardingGate.finish] returns
 * Done, with the real DeviceRegistration (phone, family numbers, duress phrase)
 * that #15's setup collects. #58/#59 read `remoteConfig.current()`.
 *
 * #59's orchestrator calls [smsChannel] when HTTP fails and there is cellular
 * signal. It and the ack plumbing are null with the flag off.
 *
 * [offlineEmergency] is the build flag OFFLINE_EMERGENCY_ENABLED (default off):
 * off, setup skips the config gate and the app never refreshes the config.
 */
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

    val onboardingGate: OnboardingGate = OnboardingGate(ConnectivityNetworkStatus(app), remoteConfig, background)

    /** Verified server acks from [com.team27.mama.emergency.sms.SmsAckReceiver]. */
    val smsAcks: SmsAckBus? = if (offlineEmergency) SmsAckBus() else null

    val smsAckFilter: SmsAckFilter? = if (offlineEmergency) SmsAckFilter(remoteConfig::current) else null

    /** Built on first use so the flag-off app never starts its scheduler thread. */
    val smsChannel: SmsChannel? by lazy {
        val acks = smsAcks ?: return@lazy null
        val scheduler = ExecutorSmsScheduler(Executors.newSingleThreadScheduledExecutor())
        SmsChannel(AndroidSmsSender(app, scheduler), { remoteConfig.current().smsNumber }, scheduler, acks)
    }

    fun deviceRegistrar(signer: PayloadSigner): DeviceRegistrar = DeviceRegistrar(serverApi, signer)

    /**
     * What the trigger (#16) runs when the server answers an alert with branch
     * `call`. #96 swaps in the CallStyle incoming-call notification that opens
     * #94's "Llamada entrante"; until then it opens the confirmation screen.
     */
    val incomingCall: IncomingCallLauncher = ConfirmationScreenLauncher(app)
}
