package com.team27.mama.onboarding

import android.content.Context
import android.net.ConnectivityManager
import android.net.NetworkCapabilities

/** Thin ConnectivityManager adapter; not unit tested (android.*). */
class ConnectivityNetworkStatus(context: Context) : NetworkStatus {
    private val manager = context.applicationContext.getSystemService(ConnectivityManager::class.java)

    override fun isOnline(): Boolean {
        val caps = manager.getNetworkCapabilities(manager.activeNetwork) ?: return false
        return caps.hasCapability(NetworkCapabilities.NET_CAPABILITY_INTERNET) &&
            caps.hasCapability(NetworkCapabilities.NET_CAPABILITY_VALIDATED)
    }
}
