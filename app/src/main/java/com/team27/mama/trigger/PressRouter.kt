package com.team27.mama.trigger

import android.app.KeyguardManager
import android.content.Context
import android.os.SystemClock
import com.team27.mama.contract.Trigger
import com.team27.mama.setup.SetupStore
import com.team27.mama.setup.SharedPreferencesStore

/** The one detector both press sources feed (accessibility and the media session). */
object PressRouter {
    private val deduper = PressDeduper()
    private val pattern = VolumePattern()

    @Synchronized
    fun onPress(context: Context, key: VolumeKey, source: PressSource) {
        // elapsedRealtime, not KeyEvent.eventTime (uptime stops in deep sleep).
        val now = SystemClock.elapsedRealtime()
        if (!deduper.accept(key, source, now)) {
            TriggerLog.log(context, "key $key from=$source duplicate")
            return
        }
        // Before setup is done there is no registered device to alert for.
        if (!SetupStore(SharedPreferencesStore.forSetup(context)).setupCompleted) {
            TriggerLog.log(context, "key $key from=$source ignored: setup not finished")
            return
        }
        val locked = context.getSystemService(KeyguardManager::class.java).isKeyguardLocked
        // Only a locked phone warms up: ordinary volume use on an unlocked
        // phone must not wake mobile data, the GPS or the server.
        if (locked) Warmup.onPress(context)
        val matched = pattern.onPress(key, now)
        TriggerLog.log(context, "key $key from=$source locked=$locked matched=$matched")
        if (matched) TriggerService.start(context, Trigger.VOLUME_PATTERN, now)
    }
}
