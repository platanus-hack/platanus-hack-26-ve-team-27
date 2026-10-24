package com.team27.mama

import android.app.Application
import com.team27.mama.emergency.config.ConfigRefresher

class MamaApp : Application() {
    lateinit var graph: AppGraph
        private set

    override fun onCreate() {
        super.onCreate()
        graph = AppGraph(this)
        // No startup refresh of its own: registerDefaultNetworkCallback reports
        // the current network right away, which is the first validated edge.
        ConfigRefresher({ graph.remoteConfig.refresh() }, graph.background).listen(this)
    }
}
