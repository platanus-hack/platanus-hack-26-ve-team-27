package com.team27.mama.relay

/**
 * Start/stop decisions for [RelayForegroundService], free of android.* so it is unit tested.
 *
 * A service started with startForegroundService must call startForeground, or Android kills the
 * app. So a stop that lands before the service reached the foreground must not call stopService:
 * it only clears [wanted], and the service stops itself right after startForeground.
 *
 * Races: the controller thread calls onStartRequested/onStopRequested, the main thread calls
 * onForegrounded/onDestroyed. Both flags are volatile. The service writes inForeground and then
 * reads wanted; stop writes wanted and then reads inForeground. Whatever the interleaving, at
 * least one side sees the other's write, so a stop is never lost. If both see it the service is
 * stopped twice, which is harmless.
 */
class ForegroundGate {
    @Volatile
    var wanted = false
        private set

    @Volatile
    var inForeground = false
        private set

    /** Call before startForegroundService. */
    fun onStartRequested() {
        wanted = true
    }

    /**
     * Call on a sticky restart (null intent) before [onForegrounded]. After process death this gate
     * is new, so [wanted] is false until the controller's first start, which may come later: with
     * the relay on, the restart is the start. With it off, nothing changes.
     */
    fun onRestarted(relayEnabled: Boolean) {
        if (relayEnabled) wanted = true
    }

    /** Returns true when stopService should be called. */
    fun onStopRequested(): Boolean {
        wanted = false
        return inForeground
    }

    /** Call after a successful startForeground. Returns false when the service must stop itself. */
    fun onForegrounded(): Boolean {
        inForeground = true
        return wanted
    }

    fun onDestroyed() {
        inForeground = false
    }
}
