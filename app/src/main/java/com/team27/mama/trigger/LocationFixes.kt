package com.team27.mama.trigger

import android.annotation.SuppressLint
import android.content.Context
import android.location.Location
import android.location.LocationListener
import android.location.LocationManager
import android.os.Build
import android.os.Bundle
import android.os.Looper
import android.os.SystemClock
import com.team27.mama.contract.Point
import kotlinx.coroutines.flow.MutableStateFlow
import kotlinx.coroutines.flow.StateFlow
import kotlinx.coroutines.flow.asStateFlow
import kotlinx.coroutines.flow.filterNotNull
import kotlinx.coroutines.flow.first
import kotlinx.coroutines.withTimeoutOrNull

/**
 * One incident's location: the newest last-known fix (the fused one on
 * Android 12+) for the immediate first point, and live fixes from the GPS
 * and the fused provider for the fresh fix and the follow-up points.
 * Call [start] once and [stop] when the incident is done.
 */
class LocationFixes(private val context: Context) {
    private val manager = context.getSystemService(LocationManager::class.java)
    private val _latest = MutableStateFlow<Location?>(null)

    /** The newest live fix since [start], null until the first one lands. */
    val latest: StateFlow<Location?> = _latest.asStateFlow()

    // Every method overridden: before Android 11 they are abstract, and a
    // lambda listener crashes on the first provider change.
    private val listener = object : LocationListener {
        override fun onLocationChanged(location: Location) {
            _latest.value = location
        }

        @Deprecated("Deprecated in Java")
        override fun onStatusChanged(provider: String?, status: Int, extras: Bundle?) = Unit

        override fun onProviderEnabled(provider: String) = Unit

        override fun onProviderDisabled(provider: String) = Unit
    }

    /** Asks the GPS and the fused provider (or the network one before Android 12) for a fix every second. */
    @SuppressLint("MissingPermission")
    fun start() {
        val providers = buildList {
            add(LocationManager.GPS_PROVIDER)
            if (Build.VERSION.SDK_INT >= Build.VERSION_CODES.S && manager.hasProvider(LocationManager.FUSED_PROVIDER)) {
                add(LocationManager.FUSED_PROVIDER)
            } else {
                add(LocationManager.NETWORK_PROVIDER)
            }
        }
        for (provider in providers) {
            try {
                if (!manager.isProviderEnabled(provider)) continue
                manager.requestLocationUpdates(provider, UPDATE_EVERY_MS, 0f, listener, Looper.getMainLooper())
            } catch (e: Exception) {
                TriggerLog.log(context, "location updates from $provider failed ${e.javaClass.simpleName}: ${e.message}")
            }
        }
    }

    fun stop() {
        runCatching { manager.removeUpdates(listener) }
    }

    /** The newest last-known fix of any enabled provider, however old. */
    @SuppressLint("MissingPermission")
    fun lastKnown(): Location? = runCatching {
        manager.getProviders(true)
            .mapNotNull { runCatching { manager.getLastKnownLocation(it) }.getOrNull() }
            .maxByOrNull { it.elapsedRealtimeNanos }
    }.getOrNull()

    /** The first live fix, or null after [timeoutMs]. */
    suspend fun awaitLive(timeoutMs: Long): Location? = withTimeoutOrNull(timeoutMs) { latest.filterNotNull().first() }

    /** The first live fix worth a LocationUpdate after [lastSent] ([AlertEvents.worthSending]), or null after [timeoutMs]. */
    suspend fun awaitWorthSending(lastSent: Location, timeoutMs: Long): Location? = withTimeoutOrNull(timeoutMs) {
        latest.filterNotNull().first { AlertEvents.worthSending(it.stamp(), lastSent.stamp()) }
    }

    private companion object {
        const val UPDATE_EVERY_MS = 1_000L
    }
}

fun Location.ageMs(): Long = (SystemClock.elapsedRealtimeNanos() - elapsedRealtimeNanos) / 1_000_000

/** A fix with no accuracy reports 0, which must not read as precise: null instead. */
fun Location.accuracyOrNull(): Float? = if (hasAccuracy()) accuracy else null

fun Location.stamp() = FixStamp(elapsedRealtimeNanos, accuracyOrNull())

/** The contract Point; speed only when the fix has one (never 0 for unknown). */
fun Location.toPoint(): Point = AlertEvents.point(
    lat = latitude,
    lng = longitude,
    accuracyM = accuracyOrNull(),
    speedMps = if (hasSpeed()) speed else null,
    timeMs = time,
)
