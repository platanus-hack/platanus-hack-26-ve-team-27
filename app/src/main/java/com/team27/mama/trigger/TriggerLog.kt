package com.team27.mama.trigger

import android.content.Context
import android.os.SystemClock
import android.util.Log
import java.io.File
import java.time.Instant

/**
 * Trigger evidence on the phone: logcat (tag MamaTrigger) plus
 * files/trigger.log, which survives a reboot and is read with
 * `adb shell run-as com.team27.mama cat files/trigger.log` (debug build).
 * Each line carries the wall clock and elapsedRealtime. The file starts over
 * past [MAX_BYTES]. Never log the duress phrase or the family numbers here.
 */
object TriggerLog {
    const val TAG = "MamaTrigger"
    private const val FILE = "trigger.log"
    private const val MAX_BYTES = 512 * 1024L

    @Synchronized
    fun log(context: Context, message: String) {
        val line = "${Instant.now()} rt=${SystemClock.elapsedRealtime()} $message"
        Log.i(TAG, line)
        runCatching {
            val file = File(context.filesDir, FILE)
            if (file.length() > MAX_BYTES) file.writeText("")
            file.appendText(line + "\n")
        }
    }
}
