package com.team27.mama

import android.content.Context
import com.team27.mama.emergency.config.RemoteConfigRepository
import com.team27.mama.emergency.config.SharedPreferencesConfigStore
import com.team27.mama.emergency.payload.PayloadSigner
import com.team27.mama.emergency.sms.OfflineAcks
import com.team27.mama.emergency.sms.SmsAckHandler
import com.team27.mama.emergency.sms.SmsChannel
import com.team27.mama.emergency.sms.SmsManagerSender
import com.team27.mama.onboarding.ConnectivityNetworkStatus
import com.team27.mama.onboarding.DeviceRegistrar
import com.team27.mama.onboarding.OnboardingGate
import com.team27.mama.server.HttpServerApi
import com.team27.mama.server.ServerApi
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

    /**
     * The SMS leg of the offline emergency (#58). Built on first use, and only #59 uses it (when
     * [offlineEmergency] is on); with the flag off nothing touches it.
     */
    val smsChannel: SmsChannel by lazy { SmsChannel(remoteConfig, SmsManagerSender(app)) }

    /**
     * Verified server acks (#58). #59 subscribes to stop the mesh and HTTP for the emergency;
     * the SMS channel is already subscribed (see [smsAckHandler]).
     */
    val acks: OfflineAcks = OfflineAcks()

    /**
     * Turns the ack SMS into an [AckInfo] on [acks], using the cached server key (null until the
     * first config fetch, so no ack is accepted before then). Used by SmsAckReceiver; the first
     * use also wires the SMS channel to stop on a matching ack.
     */
    val smsAckHandler: SmsAckHandler by lazy {
        acks.subscribe(smsChannel::onAck)
        SmsAckHandler({ remoteConfig.current().serverEd25519PublicKey }, acks)
    }

    fun deviceRegistrar(signer: PayloadSigner): DeviceRegistrar = DeviceRegistrar(serverApi, signer)
}
