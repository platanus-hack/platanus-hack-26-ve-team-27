package com.team27.mama.emergency.config

import java.util.concurrent.Executor
import org.junit.Assert.assertEquals
import org.junit.Test

class ConfigRefresherTest {
    private var count = 0
    private val refresher = ConfigRefresher({ count++ }, Executor { it.run() })

    @Test fun refreshesOnBecomingValidated() {
        refresher.onNetwork(false)
        refresher.onNetwork(true)
        assertEquals(1, count)
    }

    @Test fun noRepeatWhileStillValidated() {
        repeat(3) { refresher.onNetwork(true) }
        assertEquals(1, count)
    }

    @Test fun refreshesAgainAfterLoss() {
        refresher.onNetwork(true)
        refresher.onLost()
        refresher.onNetwork(true)
        assertEquals(2, count)
    }

    @Test fun refreshesAgainAfterUnvalidated() {
        refresher.onNetwork(true)
        refresher.onNetwork(false)
        refresher.onNetwork(true)
        assertEquals(2, count)
    }
}
