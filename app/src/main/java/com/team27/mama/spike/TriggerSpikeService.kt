package com.team27.mama.spike

import android.annotation.SuppressLint
import android.app.Notification
import android.app.NotificationChannel
import android.app.NotificationManager
import android.app.Service
import android.content.Context
import android.content.Intent
import android.content.pm.ServiceInfo
import android.location.Location
import android.location.LocationManager
import android.media.AudioFormat
import android.media.AudioRecord
import android.media.MediaRecorder
import android.os.Build
import android.os.CancellationSignal
import android.os.IBinder
import android.os.SystemClock
import androidx.core.app.ServiceCompat
import com.team27.mama.BuildConfig
import com.team27.mama.MamaApp
import com.team27.mama.R
import com.team27.mama.contract.Trigger
import com.team27.mama.emergency.OfflineFallback
import com.team27.mama.setup.SetupStore
import com.team27.mama.setup.SharedPreferencesStore
import java.io.File
import java.util.UUID
import java.util.concurrent.Executors
import kotlinx.coroutines.CompletableDeferred
import kotlinx.coroutines.CoroutineScope
import kotlinx.coroutines.Dispatchers
import kotlinx.coroutines.SupervisorJob
import kotlinx.coroutines.async
import kotlinx.coroutines.cancel
import kotlinx.coroutines.coroutineScope
import kotlinx.coroutines.delay
import kotlinx.coroutines.launch
import kotlinx.coroutines.withContext
import kotlinx.coroutines.withTimeoutOrNull
import kotlinx.serialization.json.buildJsonObject
import kotlinx.serialization.json.put

/**
 * What both triggers start: a foreground service (location, and microphone
 * when Android allows it) that sends the frozen AlertEvent with the best fix
 * it has at once, then a fresh fix as a LocationUpdate, then a spike-only
 * note with the mic and camera probes and the timings.
 */
class TriggerSpikeService : Service() {
    private val scope = CoroutineScope(SupervisorJob() + Dispatchers.IO)

    override fun onBind(intent: Intent?): IBinder? = null

    override fun onStartCommand(intent: Intent?, flags: Int, startId: Int): Int {
        val startedAt = SystemClock.elapsedRealtime()
        val trigger = intent?.getStringExtra(EXTRA_TRIGGER)?.let { runCatching { Trigger.valueOf(it) }.getOrNull() } ?: Trigger.IN_APP
        // The widget's PendingIntent has no detection time: its clock starts here.
        val detectedAt = intent?.getLongExtra(EXTRA_DETECTED_AT, -1L)?.takeIf { it > 0 } ?: startedAt
        val fgs = goForeground()
        // The widget path has no warm-up press; bring mobile data up here too.
        Cellular.request(this)
        SpikeLog.log(this, "service start trigger=$trigger fgs=$fgs sinceTriggerMs=${startedAt - detectedAt}")
        if (fgs == "none") {
            // Android would kill the app for a started-but-never-foreground service.
            stopSelf(startId)
            return START_NOT_STICKY
        }
        scope.launch {
            try {
                send(trigger, detectedAt, fgs)
            } catch (e: Exception) {
                SpikeLog.log(this@TriggerSpikeService, "send failed ${e.javaClass.simpleName}: ${e.message}")
            } finally {
                withContext(Dispatchers.Main) { stopSelf(startId) }
            }
        }
        return START_NOT_STICKY
    }

    override fun onDestroy() {
        scope.cancel()
        super.onDestroy()
    }

    /** Tries location+microphone+camera, then without camera, then location only. Returns the types it got, for the log. */
    private fun goForeground(): String {
        val manager = getSystemService(NotificationManager::class.java)
        manager.createNotificationChannel(NotificationChannel(CHANNEL, "Sincronización", NotificationManager.IMPORTANCE_LOW))
        val notification = Notification.Builder(this, CHANNEL)
            .setSmallIcon(R.drawable.ic_mama_mark)
            .setContentTitle(getString(R.string.app_name))
            .setContentText("Sincronizando")
            .build()
        val attempts = listOf(
            (
                ServiceInfo.FOREGROUND_SERVICE_TYPE_LOCATION or ServiceInfo.FOREGROUND_SERVICE_TYPE_MICROPHONE or
                    ServiceInfo.FOREGROUND_SERVICE_TYPE_CAMERA
                ) to "location|microphone|camera",
            (ServiceInfo.FOREGROUND_SERVICE_TYPE_LOCATION or ServiceInfo.FOREGROUND_SERVICE_TYPE_MICROPHONE) to "location|microphone",
            ServiceInfo.FOREGROUND_SERVICE_TYPE_LOCATION to "location",
        )
        for ((type, name) in attempts) {
            try {
                ServiceCompat.startForeground(this, NOTIFICATION_ID, notification, type)
                return name
            } catch (e: Exception) {
                SpikeLog.log(this, "startForeground($name) failed ${e.javaClass.simpleName}: ${e.message}")
            }
        }
        return "none"
    }

    private suspend fun send(trigger: Trigger, detectedAt: Long, fgs: String) = coroutineScope {
        val incidentId = UUID.randomUUID().toString()
        val deviceId = SetupStore(SharedPreferencesStore.forSetup(this@TriggerSpikeService)).deviceId
        val triggeredAtMs = System.currentTimeMillis() - (SystemClock.elapsedRealtime() - detectedAt)
        val base = BuildConfig.SERVER_URL
        val mic = async { probeMic() }
        val camera = async { CameraProbe.record(this@TriggerSpikeService, incidentId) }
        val fresh = async { freshFix() }

        // Send at once only with a fix that is recent and precise; otherwise
        // give a fresh one up to FIRST_FIX_WAIT_MS, then send what there is.
        val last = lastKnownFix()
        val lastGood = last?.takeIf { SpikeEvents.isFreshEnough(ageMs(it), if (it.hasAccuracy()) it.accuracy else null) }
        val waited = if (lastGood == null) withTimeoutOrNull(FIRST_FIX_WAIT_MS) { fresh.await() } else null
        val first = lastGood ?: waited ?: last ?: fresh.await()
        val firstChoice = when {
            first == null -> "none"
            first === lastGood -> "last_known_fresh"
            first === waited -> "waited_fresh"
            first === last -> "last_known_stale"
            else -> "fresh_late"
        }
        var alertStatus: Int? = null
        if (first == null) {
            SpikeLog.log(this@TriggerSpikeService, "no fix at all for $incidentId")
        } else {
            val body = SpikeEvents.alertBody(incidentId, deviceId, trigger, triggeredAtMs, first.toPoint())
            val sinceTrigger = SystemClock.elapsedRealtime() - detectedAt
            // Same body (same incident_id) on mobile data if the default network is slow.
            val headers = mapOf("X-Spike-Since-Trigger-Ms" to sinceTrigger.toString())
            val bytes = body.toByteArray(Charsets.UTF_8)
            val result = hedged(
                scope,
                HEDGE_AFTER_MS,
                primary = { post(SpikeEvents.incidentsUrl(base), body, sinceTrigger) },
                backup = {
                    // Read at hedge time: mobile data may still be coming up when the alert starts.
                    val cell = withTimeoutOrNull(CELLULAR_WAIT_MS) {
                        while (Cellular.network == null) delay(100)
                        Cellular.network
                    }
                    if (cell == null) {
                        SpikeLog.log(this@TriggerSpikeService, "no cellular network for the hedge")
                        -1 to 0L
                    } else {
                        SpikeHttp.post(SpikeEvents.incidentsUrl(base), bytes, JSON, headers + ("X-Spike-Path" to "cellular"), cell) {
                            SpikeLog.log(this@TriggerSpikeService, "cellular post $it")
                        }
                    }
                },
            )
            val (status, ms) = result.status to result.ms
            alertStatus = status
            SpikeLog.log(
                this@TriggerSpikeService,
                "alert $incidentId trigger=$trigger status=$status via=${result.winner} sinceTriggerAtSendMs=$sinceTrigger postMs=$ms " +
                    "fix=${first.provider} choice=$firstChoice ageMs=${ageMs(first)} acc=${first.accuracy}",
            )
        }

        // Offline emergency (#59, only with OFFLINE_EMERGENCY_ENABLED): the alert did not reach
        // the server, so SMS and the BLE mesh take over. Logs only the result and the phase.
        val offline = (application as MamaApp).graph.emergency?.takeIf { OfflineFallback.shouldDeclare(alertStatus) }
        if (offline != null) {
            SpikeLog.log(this@TriggerSpikeService, "offline emergency declared=${offline.declare()} phase=${offline.state.value.phase}")
        }

        // The warm-up's GPS fix often lands after the alert: follow with the
        // first precise one (the server keeps every point).
        val freshFix = fresh.await()?.takeIf { it !== first && it.isPrecise() }
            ?: if (first != null && !first.isPrecise()) preciseFixAfter(first) else null
        if (freshFix != null && first != null) {
            val (status, ms) = post(SpikeEvents.pointsUrl(base, incidentId), SpikeEvents.pointsBody(incidentId, deviceId, freshFix.toPoint()), SystemClock.elapsedRealtime() - detectedAt)
            SpikeLog.log(this@TriggerSpikeService, "points $incidentId status=$status postMs=$ms fix=${freshFix.provider} acc=${freshFix.accuracy}")
        }

        val micResult = mic.await()
        val cameraResult = camera.await()
        val video = File(filesDir, "spike-cam-$incidentId.mp4")
        val videoUpload = if (video.isFile && video.length() > 0) {
            val (status, ms) = SpikeHttp.post(
                SpikeEvents.videoUrl(base, incidentId),
                video.readBytes(),
                "video/mp4",
                mapOf("X-Spike-Since-Trigger-Ms" to (SystemClock.elapsedRealtime() - detectedAt).toString()),
            ) { SpikeLog.log(this@TriggerSpikeService, "video $it") }
            "status=$status bytes=${video.length()} ms=$ms"
        } else {
            "no video"
        }
        SpikeLog.log(this@TriggerSpikeService, "video upload $incidentId $videoUpload")
        val note = buildJsonObject {
            put("incident_id", incidentId)
            put("trigger", trigger.name)
            put("fgs_types", fgs)
            put("mic", micResult)
            put("camera", cameraResult)
            put("video_upload", videoUpload)
            put("first_fix_choice", firstChoice)
            put("first_fix_provider", first?.provider)
            put("first_fix_age_ms", first?.let(::ageMs))
            put("fresh_fix_provider", freshFix?.provider)
            put("fresh_fix_accuracy_m", freshFix?.accuracy)
            put("fresh_fix_ms_after_trigger", freshFix?.let { (it.elapsedRealtimeNanos / 1_000_000) - detectedAt })
            put("android", "${Build.VERSION.RELEASE} (SDK ${Build.VERSION.SDK_INT})")
            put("build", Build.VERSION.INCREMENTAL)
        }.toString()
        SpikeLog.log(this@TriggerSpikeService, "note $note")
        post(SpikeEvents.noteUrl(base), note, SystemClock.elapsedRealtime() - detectedAt)

        // Stay in the foreground while the offline emergency is still being delivered.
        if (offline != null) {
            OfflineFallback.awaitDelivered(offline)
            SpikeLog.log(this@TriggerSpikeService, "offline emergency released phase=${offline.state.value.phase}")
        }
    }

    private fun ageMs(location: Location) = (SystemClock.elapsedRealtimeNanos() - location.elapsedRealtimeNanos) / 1_000_000

    /** A fix with no accuracy reports 0, which must not read as precise. */
    private fun Location.isPrecise() = hasAccuracy() && SpikeEvents.isPrecise(accuracy)

    private fun Location.toPoint() = SpikeEvents.point(
        lat = latitude,
        lng = longitude,
        accuracyM = if (hasAccuracy()) accuracy else null,
        speedMps = if (hasSpeed()) speed else null,
        timeMs = time,
    )

    @SuppressLint("MissingPermission")
    private fun lastKnownFix(): Location? {
        val lm = getSystemService(LocationManager::class.java)
        return lm.getProviders(true)
            .mapNotNull { runCatching { lm.getLastKnownLocation(it) }.getOrNull() }
            .maxByOrNull { it.elapsedRealtimeNanos }
    }

    /** Polls the last known fixes for a precise one newer than [after], up to [PRECISE_WAIT_MS]. */
    private suspend fun preciseFixAfter(after: Location): Location? = withTimeoutOrNull(PRECISE_WAIT_MS) {
        var fix: Location? = null
        while (fix == null) {
            fix = lastKnownFix()?.takeIf { it.elapsedRealtimeNanos > after.elapsedRealtimeNanos && it.isPrecise() }
            if (fix == null) delay(500)
        }
        fix
    }

    /** The first fresh fix from GPS or the fused provider, or null after [FIX_TIMEOUT_MS]. */
    @SuppressLint("MissingPermission")
    private suspend fun freshFix(): Location? {
        val lm = getSystemService(LocationManager::class.java)
        val providers = buildList {
            add(LocationManager.GPS_PROVIDER)
            if (Build.VERSION.SDK_INT >= Build.VERSION_CODES.S && lm.hasProvider(LocationManager.FUSED_PROVIDER)) add(LocationManager.FUSED_PROVIDER)
        }.filter { lm.isProviderEnabled(it) }
        if (providers.isEmpty() || Build.VERSION.SDK_INT < Build.VERSION_CODES.R) return null
        val result = CompletableDeferred<Location?>()
        val cancel = CancellationSignal()
        val executor = Executors.newSingleThreadExecutor()
        var pending = providers.size
        providers.forEach { provider ->
            lm.getCurrentLocation(provider, cancel, executor) { location ->
                SpikeLog.log(this, "current fix from $provider: ${location?.let { "acc=${it.accuracy}" } ?: "null"}")
                if (location != null) result.complete(location) else if (--pending == 0) result.complete(null)
            }
        }
        return try {
            withTimeoutOrNull(FIX_TIMEOUT_MS) { result.await() }
        } finally {
            cancel.cancel()
            executor.shutdown()
        }
    }

    /** Records 1 s and reports whether Android delivered real audio or silenced it. */
    @SuppressLint("MissingPermission")
    private fun probeMic(): String {
        val rate = 16_000
        val min = AudioRecord.getMinBufferSize(rate, AudioFormat.CHANNEL_IN_MONO, AudioFormat.ENCODING_PCM_16BIT)
        val recorder = try {
            AudioRecord(MediaRecorder.AudioSource.MIC, rate, AudioFormat.CHANNEL_IN_MONO, AudioFormat.ENCODING_PCM_16BIT, maxOf(min, rate * 2))
        } catch (e: Exception) {
            return "create failed ${e.javaClass.simpleName}: ${e.message}"
        }
        try {
            if (recorder.state != AudioRecord.STATE_INITIALIZED) return "not initialized"
            recorder.startRecording()
            val buffer = ShortArray(rate)
            var read = 0
            while (read < buffer.size) {
                val n = recorder.read(buffer, read, buffer.size - read)
                if (n <= 0) return "read error $n after $read samples"
                read += n
            }
            val silenced = if (Build.VERSION.SDK_INT >= Build.VERSION_CODES.Q) recorder.activeRecordingConfiguration?.isClientSilenced else null
            return "peak=${SpikeEvents.peakAmplitude(buffer, read)} samples=$read clientSilenced=$silenced"
        } catch (e: Exception) {
            return "record failed ${e.javaClass.simpleName}: ${e.message}"
        } finally {
            runCatching { recorder.stop() }
            recorder.release()
        }
    }

    /** POSTs JSON through [SpikeHttp]; returns the status (-1 on a network error) and the round trip in ms. */
    private fun post(url: String, body: String, sinceTriggerMs: Long): Pair<Int, Long> =
        SpikeHttp.post(
            url,
            body.toByteArray(Charsets.UTF_8),
            JSON,
            mapOf("X-Spike-Since-Trigger-Ms" to sinceTriggerMs.toString()),
        ) { SpikeLog.log(this, "post $it") }

    companion object {
        private const val CHANNEL = "mama_sync"
        private const val NOTIFICATION_ID = 10
        private const val FIX_TIMEOUT_MS = 30_000L
        private const val FIRST_FIX_WAIT_MS = 1_000L
        private const val PRECISE_WAIT_MS = 10_000L
        private const val HEDGE_AFTER_MS = 1_500L
        private const val CELLULAR_WAIT_MS = 3_000L
        private const val JSON = "application/json; charset=utf-8"
        private const val EXTRA_TRIGGER = "trigger"
        private const val EXTRA_DETECTED_AT = "detected_at_elapsed"

        fun intent(context: Context, trigger: Trigger, detectedAtElapsed: Long? = null): Intent =
            Intent(context, TriggerSpikeService::class.java)
                .putExtra(EXTRA_TRIGGER, trigger.name)
                .apply { if (detectedAtElapsed != null) putExtra(EXTRA_DETECTED_AT, detectedAtElapsed) }

        fun start(context: Context, trigger: Trigger, detectedAtElapsed: Long) {
            try {
                context.startForegroundService(intent(context, trigger, detectedAtElapsed))
            } catch (e: Exception) {
                SpikeLog.log(context, "startForegroundService failed ${e.javaClass.simpleName}: ${e.message}")
            }
        }
    }
}
