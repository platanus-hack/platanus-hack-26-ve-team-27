package com.team27.mama.spike

import android.content.Context
import android.os.SystemClock
import android.util.Log
import java.io.File
import java.time.Instant

/**
 * Spike evidence on the phone: logcat (tag MamaSpike) plus files/spike.log,
 * which survives a reboot and is read with
 * `adb shell run-as com.team27.mama cat files/spike.log` (debug build).
 * Each line carries the wall clock and elapsedRealtime.
 */
object SpikeLog {
    const val TAG = "MamaSpike"

    @Synchronized
    fun log(context: Context, message: String) {
        val line = "${Instant.now()} rt=${SystemClock.elapsedRealtime()} $message"
        Log.i(TAG, line)
        runCatching { File(context.filesDir, "spike.log").appendText(line + "\n") }
    }
}
