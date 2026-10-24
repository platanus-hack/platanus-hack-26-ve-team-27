package com.team27.mama.emergency

import android.Manifest
import android.content.Context
import android.content.pm.PackageManager
import android.location.Location
import android.location.LocationListener
import android.location.LocationManager
import android.os.Bundle
import android.os.Looper
import androidx.core.content.ContextCompat
import kotlinx.coroutines.suspendCancellableCoroutine

/**
 * [LocationManager]-only location (no Play Services). Needs ACCESS_FINE_LOCATION or
 * ACCESS_COARSE_LOCATION; without either both calls return null. Not unit tested (framework).
 * Consumer: `AppGraph.emergency`.
 */
class AndroidLocationSource(context: Context) : LocationSource {
    private val app = context.applicationContext
    private val manager = app.getSystemService(Context.LOCATION_SERVICE) as? LocationManager

    private fun hasPermission(): Boolean =
        listOf(Manifest.permission.ACCESS_FINE_LOCATION, Manifest.permission.ACCESS_COARSE_LOCATION).any {
            ContextCompat.checkSelfPermission(app, it) == PackageManager.PERMISSION_GRANTED
        }

    override fun lastKnown(): LocationFix? {
        val lm = manager ?: return null
        if (!hasPermission()) return null
        return try {
            listOf(LocationManager.GPS_PROVIDER, LocationManager.NETWORK_PROVIDER, LocationManager.PASSIVE_PROVIDER)
                .mapNotNull { runCatching { lm.getLastKnownLocation(it) }.getOrNull() }
                .maxByOrNull { it.time }
                ?.toFix()
        } catch (e: SecurityException) {
            null
        }
    }

    override suspend fun awaitFreshFix(): LocationFix? {
        val lm = manager ?: return null
        if (!hasPermission()) return null
        val providers = listOf(LocationManager.GPS_PROVIDER, LocationManager.NETWORK_PROVIDER)
            .filter { runCatching { lm.isProviderEnabled(it) }.getOrDefault(false) }
        if (providers.isEmpty()) return null
        return suspendCancellableCoroutine { cont ->
            val listener = object : LocationListener {
                override fun onLocationChanged(location: Location) {
                    lm.removeUpdates(this)
                    if (cont.isActive) cont.resume(location.toFix()) { }
                }

                @Deprecated("Deprecated in API 29")
                override fun onStatusChanged(provider: String?, status: Int, extras: Bundle?) = Unit
                override fun onProviderEnabled(provider: String) = Unit
                override fun onProviderDisabled(provider: String) = Unit
            }
            cont.invokeOnCancellation { lm.removeUpdates(listener) }
            try {
                providers.forEach { lm.requestLocationUpdates(it, 0L, 0f, listener, Looper.getMainLooper()) }
            } catch (e: SecurityException) {
                lm.removeUpdates(listener)
                if (cont.isActive) cont.resume(null) { }
            }
        }
    }

    private fun Location.toFix() = LocationFix(latitude, longitude, time)
}
