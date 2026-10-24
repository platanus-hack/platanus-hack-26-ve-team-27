package com.team27.mama

import android.app.Application
import android.util.Log
import com.team27.mama.emergency.config.ConfigRefresher
import com.team27.mama.mesh.MeshGateway
import com.team27.mama.relay.AppVisibility
import com.team27.mama.relay.MeshController
import com.team27.mama.relay.RelayForegroundService
import com.team27.mama.relay.keepsMeshOn
import kotlinx.coroutines.CoroutineScope
import kotlinx.coroutines.Dispatchers
import kotlinx.coroutines.SupervisorJob
import kotlinx.coroutines.flow.flowOf
import kotlinx.coroutines.flow.map

class MamaApp : Application() {
    lateinit var graph: AppGraph
        private set

    override fun onCreate() {
        super.onCreate()
        graph = AppGraph(this)
        if (!graph.offlineEmergency) return
        // No startup refresh of its own: registerDefaultNetworkCallback reports
        // the current network right away, which is the first validated edge.
        ConfigRefresher({ graph.remoteConfig.refresh() }, graph.background).listen(this)
        startRelay()
    }

    /** #60. Order matters: the relay subscribes to `broadcasts` (no replay) before the controller can start the gateway. */
    private fun startRelay() {
        val relay = graph.relay ?: return
        val settings = graph.relaySettings ?: return
        AppVisibility.install()
        relay.start()
        ConfigRefresher(refresh = relay::onNetworkAvailable, executor = Runnable::run).listen(this)
        MeshController(
            appVisible = AppVisibility.visible,
            relayEnabled = settings.enabled,
            emergencyActive = graph.emergency?.state?.map { it.phase.keepsMeshOn() } ?: flowOf(false),
            startGateway = { MeshGateway.getInstance(this).start() },
            stopGateway = { MeshGateway.getInstance(this).stop() },
            startService = { RelayForegroundService.start(this) },
            stopService = { RelayForegroundService.stop(this) },
            scope = CoroutineScope(SupervisorJob() + Dispatchers.Main.immediate),
            onError = { Log.w("MamaRelay", it) },
        ).start()
    }
}
