package com.team27.mama.trigger

import android.Manifest
import android.app.Service
import android.content.Context
import android.content.Intent
import android.content.pm.PackageManager
import android.content.pm.ServiceInfo
import android.location.Location
import android.os.Build
import android.os.Handler
import android.os.IBinder
import android.os.Looper
import android.os.SystemClock
import androidx.core.app.ServiceCompat
import androidx.core.content.ContextCompat
import com.team27.mama.BuildConfig
import com.team27.mama.MamaApp
import com.team27.mama.contract.Trigger
import com.team27.mama.emergency.OfflineFallback
import com.team27.mama.setup.RegistrationClient
import com.team27.mama.setup.RegistrationResult
import com.team27.mama.setup.SetupStore
import com.team27.mama.setup.SharedPreferencesStore
import java.util.UUID
import kotlinx.coroutines.CoroutineScope
import kotlinx.coroutines.Dispatchers
import kotlinx.coroutines.Job
import kotlinx.coroutines.SupervisorJob
import kotlinx.coroutines.cancel
import kotlinx.coroutines.delay
import kotlinx.coroutines.launch
import kotlinx.coroutines.withTimeoutOrNull

/**
 * What every trigger starts (volume pattern, widget, in-app): a foreground
 * service of type location that, for one incident,
 * 1. takes the first point at once: the newest last-known fix when it is
 *    recent and precise, else the first live fix within 1 s, else the stale
 *    one (the "fresh GPS" findings of spike #10, spikes/trigger/RESULTS.md);
 * 2. posts the AlertEvent, hedged on mobile data after 1.5 s and retried with
 *    the same incident_id ([AlertSender]);
 * 3. hands off on the server's branch ([handOff]): `call` rings through
 *    [IncomingCallLauncher], the other branches open the confirmation screen;
 * 4. follows with fresh fixes as LocationUpdates for [FOLLOW_UP_MS];
 * 5. when the alert did not reach the server, declares the offline emergency
 *    ([offlineFallback], only with OFFLINE_EMERGENCY_ENABLED).
 *
 * It only runs once setup is done, one incident at a time. It never opens
 * the camera or records audio (has_audio is false).
 */
class TriggerService : Service() {
    private val scope = CoroutineScope(SupervisorJob() + Dispatchers.IO)
    private val main = Handler(Looper.getMainLooper())

    // Main thread only.
    private var incident: Job? = null
    private var lastStartId = 0

    override fun onBind(intent: Intent?): IBinder? = null

    override fun onStartCommand(intent: Intent?, flags: Int, startId: Int): Int {
        lastStartId = startId
        val startedAt = SystemClock.elapsedRealtime()
        val trigger = intent?.getStringExtra(EXTRA_TRIGGER)?.let { runCatching { Trigger.valueOf(it) }.getOrNull() } ?: Trigger.IN_APP
        // The widget's PendingIntent has no detection time: its clock starts here.
        val detectedAt = intent?.getLongExtra(EXTRA_DETECTED_AT, -1L)?.takeIf { it > 0 } ?: startedAt
        // Every startForegroundService() needs a startForeground(), even for a trigger this service ignores.
        val fgs = goForeground()
        val store = SetupStore(SharedPreferencesStore.forSetup(this))
        TriggerLog.log(this, "start trigger=$trigger fgs=$fgs sinceTriggerMs=${startedAt - detectedAt}")
        when {
            incident?.isActive == true -> TriggerLog.log(this, "an incident is running; trigger ignored")
            fgs != FGS_LOCATION -> stopIfIdle("no location foreground service")
            !hasLocationPermission() -> stopIfIdle("no location permission")
            !store.setupCompleted -> stopIfIdle("setup not finished")
            else -> {
                // The widget and in-app paths have no warm-up press: bring mobile data up here.
                Cellular.request(this)
                val job = scope.launch { runIncident(trigger, detectedAt, store) }
                incident = job
                job.invokeOnCompletion {
                    main.post {
                        if (incident === job) {
                            incident = null
                            stopSelf(lastStartId)
                        }
                    }
                }
            }
        }
        return START_NOT_STICKY
    }

    override fun onDestroy() {
        scope.cancel()
        main.removeCallbacksAndMessages(null)
        super.onDestroy()
    }

    private fun stopIfIdle(reason: String) {
        TriggerLog.log(this, "$reason; nothing sent")
        if (incident?.isActive != true) stopSelf(lastStartId)
    }

    private fun hasLocationPermission() =
        listOf(Manifest.permission.ACCESS_FINE_LOCATION, Manifest.permission.ACCESS_COARSE_LOCATION)
            .any { ContextCompat.checkSelfPermission(this, it) == PackageManager.PERMISSION_GRANTED }

    /**
     * Location when Android allows it. Without the location permission (setup
     * not done) a short service still satisfies startForegroundService() on
     * Android 14+, so the service can stop without crashing the app.
     */
    private fun goForeground(): String? {
        val notification = ForegroundNotification.build(this, "Sincronizando")
        val attempts = buildList {
            add(ServiceInfo.FOREGROUND_SERVICE_TYPE_LOCATION to FGS_LOCATION)
            if (Build.VERSION.SDK_INT >= Build.VERSION_CODES.UPSIDE_DOWN_CAKE) {
                add(ServiceInfo.FOREGROUND_SERVICE_TYPE_SHORT_SERVICE to "shortService")
            }
        }
        for ((type, name) in attempts) {
            try {
                ServiceCompat.startForeground(this, NOTIFICATION_ID, notification, type)
                return name
            } catch (e: Exception) {
                TriggerLog.log(this, "startForeground($name) failed ${e.javaClass.simpleName}: ${e.message}")
            }
        }
        return null
    }

    private suspend fun runIncident(trigger: Trigger, detectedAt: Long, store: SetupStore) {
        val incidentId = UUID.randomUUID().toString()
        val deviceId = store.deviceId
        // Remembered from here on so "Fue sin querer" can cancel it on the server, even after this service ends.
        lastIncident = IncidentRef(incidentId, deviceId)
        val triggeredAtMs = System.currentTimeMillis() - (SystemClock.elapsedRealtime() - detectedAt)
        val base = BuildConfig.SERVER_URL
        val graph = (application as MamaApp).graph
        val override = graph.channelOverride.value
        if (OfflineFallback.skipsOnlineAlert(override, graph.emergency != null)) {
            TriggerLog.log(this, "alert $incidentId skipped: channel override $override")
            offlineFallback(null)
            return
        }
        val fixes = LocationFixes(this)
        fixes.start()
        // The AlertEvent's HTTP status for the offline fallback: null when nothing was sent.
        val alertStatus: Int? = try {
            val fix = firstFix(fixes)
            if (fix == null) {
                TriggerLog.log(this, "no fix at all for $incidentId; nothing sent")
                null
            } else {
                val (first, choice) = fix
                val event = AlertEvents.alertEvent(incidentId, deviceId, trigger, triggeredAtMs, listOf(first.toPoint()))
                TriggerLog.log(
                    this,
                    "alert $incidentId trigger=$trigger sinceTriggerAtSendMs=${SystemClock.elapsedRealtime() - detectedAt} " +
                        "fix=${first.provider} choice=$choice ageMs=${first.ageMs()} acc=${first.accuracyOrNull()} speed=${event.points.last().speedMps}",
                )
                val outcome = AlertSender(
                    post = { body -> postAlert(base, body) },
                    reregister = { reregister(store) },
                    onAttempt = { attempt, result ->
                        TriggerLog.log(
                            this,
                            "alert $incidentId attempt=$attempt status=${result.status} postMs=${result.ms} " +
                                "sinceTriggerMs=${SystemClock.elapsedRealtime() - detectedAt}",
                        )
                    },
                ).send(event)
                val handoff = handOff(incidentId, outcome, (application as MamaApp).graph.incomingCall) {
                    TriggerScreens.openConfirmation(this, it)
                }
                TriggerLog.log(this, "alert $incidentId outcome=$outcome handoff=$handoff")
                if (outcome is AlertOutcome.Accepted) followUp(base, incidentId, deviceId, first, fixes)
                when (outcome) {
                    is AlertOutcome.Accepted -> 200
                    is AlertOutcome.Rejected -> outcome.status
                    is AlertOutcome.GaveUp -> outcome.lastStatus
                }
            }
        } finally {
            fixes.stop()
        }
        offlineFallback(alertStatus)
    }

    /**
     * Offline emergency (#59, only with OFFLINE_EMERGENCY_ENABLED): the alert did not reach the
     * server, so SMS and the BLE mesh take over. The service stays in the foreground while the
     * emergency is being delivered. Logs only the result and the phase.
     */
    private suspend fun offlineFallback(alertStatus: Int?) {
        val offline = (application as MamaApp).graph.emergency?.takeIf { OfflineFallback.shouldDeclare(alertStatus) } ?: return
        TriggerLog.log(this, "offline emergency declared=${offline.declare()} phase=${offline.state.value.phase}")
        OfflineFallback.awaitDelivered(offline)
        TriggerLog.log(this, "offline emergency released phase=${offline.state.value.phase}")
    }

    /** The first point and how it was picked (for the log), or null with no fix at all after [FIX_TIMEOUT_MS]. */
    private suspend fun firstFix(fixes: LocationFixes): Pair<Location, String>? {
        val last = fixes.lastKnown()
        if (last != null && AlertEvents.isFreshEnough(last.ageMs(), last.accuracyOrNull())) return last to "last_known_fresh"
        fixes.awaitLive(FIRST_FIX_WAIT_MS)?.let { return it to "live" }
        if (last != null) return last to "last_known_stale"
        return fixes.awaitLive(FIX_TIMEOUT_MS)?.let { it to "live_late" }
    }

    /** One attempt of POST /incidents; the same body goes on mobile data if the default network has no 2xx after [HEDGE_AFTER_MS]. */
    private suspend fun postAlert(base: String, body: String): HttpResult {
        val url = AlertEvents.incidentsUrl(base)
        val result = hedged(
            scope,
            HEDGE_AFTER_MS,
            primary = { TriggerHttp.postJson(url, body) { TriggerLog.log(this, "post $it") } },
            backup = {
                // Read at hedge time: mobile data may still be coming up when the alert starts.
                val cell = withTimeoutOrNull(CELLULAR_WAIT_MS) {
                    while (Cellular.network == null) delay(100)
                    Cellular.network
                }
                if (cell == null) {
                    TriggerLog.log(this, "no cellular network for the hedge")
                    HttpResult.NETWORK_ERROR
                } else {
                    TriggerHttp.postJson(url, body, cell) { TriggerLog.log(this, "cellular post $it") }
                }
            },
        )
        TriggerLog.log(this, "alert post via=${result.winner} status=${result.result.status}")
        return result.result
    }

    /** POST /devices again with what setup saved (the server answered 422: it does not know this device). */
    private suspend fun reregister(store: SetupStore): Boolean {
        val registration = store.registration(BuildConfig.VERSION_NAME) ?: return false
        val result = RegistrationClient(BuildConfig.SERVER_URL).register(registration)
        TriggerLog.log(this, "re-registered device: ${result is RegistrationResult.Registered}")
        return result is RegistrationResult.Registered
    }

    /** Sends each fix worth it ([AlertEvents.worthSending]) as a LocationUpdate, at most one per [FOLLOW_UP_EVERY_MS], for [FOLLOW_UP_MS]. */
    private suspend fun followUp(base: String, incidentId: String, deviceId: String, first: Location, fixes: LocationFixes) {
        val url = AlertEvents.pointsUrl(base, incidentId)
        val endAt = SystemClock.elapsedRealtime() + FOLLOW_UP_MS
        var lastSent = first
        var sent = 0
        while (true) {
            val remaining = endAt - SystemClock.elapsedRealtime()
            if (remaining <= 0) break
            val next = fixes.awaitWorthSending(lastSent, remaining) ?: break
            val point = next.toPoint()
            val result = TriggerHttp.postJson(url, AlertEvents.pointsBody(incidentId, deviceId, listOf(point))) {
                TriggerLog.log(this, "points $it")
            }
            TriggerLog.log(
                this,
                "points $incidentId status=${result.status} postMs=${result.ms} fix=${next.provider} " +
                    "acc=${point.accuracyM} speed=${point.speedMps}",
            )
            if (result.ok) {
                lastSent = next
                sent++
            }
            delay(FOLLOW_UP_EVERY_MS)
        }
        TriggerLog.log(this, "follow-up done for $incidentId points=$sent")
    }

    companion object {
        private const val NOTIFICATION_ID = 10
        private const val FGS_LOCATION = "location"
        private const val FIRST_FIX_WAIT_MS = 1_000L
        private const val FIX_TIMEOUT_MS = 30_000L
        private const val HEDGE_AFTER_MS = 1_500L
        private const val CELLULAR_WAIT_MS = 3_000L
        private const val FOLLOW_UP_MS = 120_000L
        private const val FOLLOW_UP_EVERY_MS = 10_000L
        private const val CANCEL_ATTEMPTS = 3
        private const val CANCEL_RETRY_MS = 2_000L
        private const val EXTRA_TRIGGER = "trigger"
        private const val EXTRA_DETECTED_AT = "detected_at_elapsed"

        fun intent(context: Context, trigger: Trigger, detectedAtElapsed: Long? = null): Intent =
            Intent(context, TriggerService::class.java)
                .putExtra(EXTRA_TRIGGER, trigger.name)
                .apply { if (detectedAtElapsed != null) putExtra(EXTRA_DETECTED_AT, detectedAtElapsed) }

        /** The newest incident this process started: what "Fue sin querer" cancels on the server. */
        @Volatile
        private var lastIncident: IncidentRef? = null

        /** Outlives the service, which is destroyed by [cancel] before its request is done. */
        private val cancelScope = CoroutineScope(SupervisorJob() + Dispatchers.IO)

        /**
         * Stops the running incident ("false alarm" on the confirmation screen): the service dies, which
         * cancels the coroutine scope, so the follow-up location posts stop and the foreground
         * notification goes away. A no-op when no service runs. It also asks the server to cancel the
         * incident (its call, retries and unsent family message), best effort: a failure is only logged.
         */
        fun cancel(context: Context) {
            val app = context.applicationContext
            val incident = lastIncident
            lastIncident = null
            val stopped = context.stopService(Intent(context, TriggerService::class.java))
            TriggerLog.log(context, "incident cancelled by the person; service stopped=$stopped")
            if (incident == null) return
            cancelScope.launch { cancelOnServer(app, BuildConfig.SERVER_URL, incident) }
        }

        /** POSTs the cancel, retrying a network error or a 5xx up to [CANCEL_ATTEMPTS] times. */
        private suspend fun cancelOnServer(context: Context, base: String, incident: IncidentRef) {
            val url = AlertEvents.cancelUrl(base, incident.incidentId)
            val body = AlertEvents.cancelBody(incident.incidentId, incident.deviceId)
            repeat(CANCEL_ATTEMPTS) { attempt ->
                val result = TriggerHttp.postJson(url, body) { TriggerLog.log(context, it) }
                TriggerLog.log(context, "cancel ${incident.incidentId} attempt=${attempt + 1} status=${result.status}")
                if (result.status in 200..499) return
                delay(CANCEL_RETRY_MS)
            }
        }

        /**
         * Starts an alert. [detectedAtElapsed] is when the trigger was
         * detected (elapsedRealtime). An in-app button calls
         * `start(context, Trigger.IN_APP)`.
         */
        fun start(context: Context, trigger: Trigger, detectedAtElapsed: Long = SystemClock.elapsedRealtime()) {
            try {
                context.startForegroundService(intent(context, trigger, detectedAtElapsed))
            } catch (e: Exception) {
                TriggerLog.log(context, "startForegroundService failed ${e.javaClass.simpleName}: ${e.message}")
            }
        }
    }
}

/** The incident and device ids a cancel request needs. */
private data class IncidentRef(val incidentId: String, val deviceId: String)
