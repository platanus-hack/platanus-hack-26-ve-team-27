package com.team27.mama.emergency.config

import android.content.Context
import android.net.ConnectivityManager
import android.net.Network
import android.net.NetworkCapabilities
import java.util.concurrent.Executor

/** Runs [refresh] on [executor] each time the network goes from not validated to validated. */
class ConfigRefresher(
    private val refresh: () -> Unit,
    private val executor: Executor,
) {
    private var validated = false

    @Synchronized
    fun onNetwork(validated: Boolean) {
        val becameValid = validated && !this.validated
        this.validated = validated
        if (becameValid) executor.execute(refresh)
    }

    fun onLost() = onNetwork(false)

    /** Android adapter; not unit tested. */
    fun listen(context: Context) {
        val manager = context.applicationContext.getSystemService(ConnectivityManager::class.java)
        manager.registerDefaultNetworkCallback(object : ConnectivityManager.NetworkCallback() {
            override fun onCapabilitiesChanged(network: Network, networkCapabilities: NetworkCapabilities) {
                onNetwork(networkCapabilities.hasCapability(NetworkCapabilities.NET_CAPABILITY_VALIDATED))
            }

            override fun onLost(network: Network) = this@ConfigRefresher.onLost()
        })
    }
}
