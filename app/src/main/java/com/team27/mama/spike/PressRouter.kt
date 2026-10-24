package com.team27.mama.spike

import android.app.KeyguardManager
import android.content.Context
import android.os.PowerManager
import android.os.SystemClock
import com.team27.mama.contract.Trigger

/** The one detector both press sources feed (accessibility and the media session). */
object PressRouter {
    private val deduper = PressDeduper()
    private val pattern = VolumePattern()

    @Synchronized
    fun onPress(context: Context, key: VolumeKey, source: PressSource) {
        // elapsedRealtime, not KeyEvent.eventTime (uptime stops in deep sleep).
        val now = SystemClock.elapsedRealtime()
        if (!deduper.accept(key, source, now)) {
            SpikeLog.log(context, "key $key from=$source duplicate")
            return
        }
        Warmup.onPress(context)
        val interactive = context.getSystemService(PowerManager::class.java).isInteractive
        val locked = context.getSystemService(KeyguardManager::class.java).isKeyguardLocked
        val matched = pattern.onPress(key, now)
        SpikeLog.log(context, "key $key from=$source screenOn=$interactive locked=$locked matched=$matched")
        if (matched) TriggerSpikeService.start(context, Trigger.VOLUME_PATTERN, now)
    }
}
