package com.team27.mama

import android.app.Application
import com.team27.mama.emergency.config.ConfigRefresher
import java.util.concurrent.Executors

class MamaApp : Application() {
    lateinit var graph: AppGraph
        private set

    override fun onCreate() {
        super.onCreate()
        graph = AppGraph(this)
        val executor = Executors.newSingleThreadExecutor()
        executor.execute { graph.remoteConfig.refresh() }
        ConfigRefresher({ graph.remoteConfig.refresh() }, executor).listen(this)
    }
}
