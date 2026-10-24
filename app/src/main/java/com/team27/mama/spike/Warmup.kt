package com.team27.mama.spike

import android.annotation.SuppressLint
import android.content.Context
import android.location.LocationManager
import android.os.Build
import android.os.CancellationSignal
import android.os.Handler
import android.os.Looper
import android.os.SystemClock
import com.team27.mama.BuildConfig
import java.util.concurrent.Executors

/**
 * On the first press of a possible pattern, wakes the network (one GET that
 * opens a keep-alive connection) and the GPS, so the ~1 s the person takes
 * for the other two presses pays the cold-start cost instead of the alert.
 * At most once per [MIN_INTERVAL_MS].
 */
object Warmup {
    private const val MIN_INTERVAL_MS = 15_000L
    private const val GPS_WARM_MS = 20_000L
    private val executor = Executors.newSingleThreadExecutor()
    private var lastAt = -MIN_INTERVAL_MS

    @Synchronized
    fun onPress(context: Context) {
        val now = SystemClock.elapsedRealtime()
        if (now - lastAt < MIN_INTERVAL_MS) return
        lastAt = now
        val app = context.applicationContext
        warmGps(app)
        executor.execute {
            val (status, ms) = SpikeHttp.warm(BuildConfig.SERVER_URL) { SpikeLog.log(app, "warm $it") }
            SpikeLog.log(app, "warm net status=$status ms=$ms")
        }
    }

    /** A GPS request whose fix lands in getLastKnownLocation for the alert. */
    @SuppressLint("MissingPermission")
    private fun warmGps(context: Context) {
        if (Build.VERSION.SDK_INT < Build.VERSION_CODES.R) return
        val lm = context.getSystemService(LocationManager::class.java)
        if (!lm.isProviderEnabled(LocationManager.GPS_PROVIDER)) return
        val cancel = CancellationSignal()
        try {
            lm.getCurrentLocation(LocationManager.GPS_PROVIDER, cancel, executor) { location ->
                SpikeLog.log(context, "warm gps ${location?.let { "acc=${it.accuracy}" } ?: "null"}")
            }
            Handler(Looper.getMainLooper()).postDelayed({ cancel.cancel() }, GPS_WARM_MS)
        } catch (e: Exception) {
            SpikeLog.log(context, "warm gps failed ${e.javaClass.simpleName}: ${e.message}")
        }
    }
}
