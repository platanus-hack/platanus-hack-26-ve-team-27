package com.team27.mama.spike

import android.content.Context
import android.net.ConnectivityManager
import android.net.Network
import android.net.NetworkCapabilities
import android.net.NetworkRequest
import android.os.Handler
import android.os.Looper

/**
 * Brings up mobile data next to Wi-Fi so a slow Wi-Fi request can be hedged
 * on it (the venue Wi-Fi once took 20 s for one request). Needs mobile data
 * turned on; Android keeps the Wi-Fi as the default network.
 */
object Cellular {
    private const val HOLD_MS = 90_000L
    private val handler = Handler(Looper.getMainLooper())

    @Volatile
    var network: Network? = null
        private set
    private var callback: ConnectivityManager.NetworkCallback? = null

    /** Requests a cellular network and keeps it for [HOLD_MS]; a new call extends the hold. */
    @Synchronized
    fun request(context: Context) {
        val app = context.applicationContext
        handler.removeCallbacksAndMessages(null)
        handler.postDelayed({ release(app) }, HOLD_MS)
        if (callback != null) return
        val cm = app.getSystemService(ConnectivityManager::class.java)
        val cb = object : ConnectivityManager.NetworkCallback() {
            override fun onAvailable(n: Network) {
                network = n
                SpikeLog.log(app, "cellular available")
            }

            override fun onLost(n: Network) {
                if (network == n) network = null
                SpikeLog.log(app, "cellular lost")
            }
        }
        val request = NetworkRequest.Builder()
            .addTransportType(NetworkCapabilities.TRANSPORT_CELLULAR)
            .addCapability(NetworkCapabilities.NET_CAPABILITY_INTERNET)
            .build()
        try {
            cm.requestNetwork(request, cb)
            callback = cb
        } catch (e: Exception) {
            SpikeLog.log(app, "cellular request failed ${e.javaClass.simpleName}: ${e.message}")
        }
    }

    @Synchronized
    private fun release(context: Context) {
        callback?.let { runCatching { context.getSystemService(ConnectivityManager::class.java).unregisterNetworkCallback(it) } }
        callback = null
        network = null
    }
}
