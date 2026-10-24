package com.team27.mama.spike

import android.accessibilityservice.AccessibilityService
import android.app.KeyguardManager
import android.content.Intent
import android.os.PowerManager
import android.os.SystemClock
import android.view.KeyEvent
import android.view.accessibility.AccessibilityEvent
import com.team27.mama.contract.Trigger

/**
 * Watches the volume keys for up-down-up and starts [TriggerSpikeService].
 * It never consumes a key, so volume and the system shortcuts behave as
 * usual. Logs every volume press with the screen and lock state, which is
 * the evidence for "locked, screen off".
 */
class VolumePatternService : AccessibilityService() {
    private val pattern = VolumePattern()

    override fun onServiceConnected() {
        SpikeLog.log(this, "a11y connected")
    }

    override fun onKeyEvent(event: KeyEvent): Boolean {
        val key = when (event.keyCode) {
            KeyEvent.KEYCODE_VOLUME_UP -> VolumeKey.UP
            KeyEvent.KEYCODE_VOLUME_DOWN -> VolumeKey.DOWN
            else -> return false
        }
        if (event.action != KeyEvent.ACTION_DOWN || event.repeatCount != 0) return false
        // elapsedRealtime, not event.eventTime (uptime stops in deep sleep).
        val now = SystemClock.elapsedRealtime()
        val interactive = getSystemService(PowerManager::class.java).isInteractive
        val locked = getSystemService(KeyguardManager::class.java).isKeyguardLocked
        val matched = pattern.onPress(key, now)
        SpikeLog.log(this, "key $key screenOn=$interactive locked=$locked matched=$matched")
        if (matched) TriggerSpikeService.start(this, Trigger.VOLUME_PATTERN, now)
        return false
    }

    override fun onAccessibilityEvent(event: AccessibilityEvent?) = Unit

    override fun onInterrupt() = Unit

    override fun onUnbind(intent: Intent?): Boolean {
        SpikeLog.log(this, "a11y unbound")
        return super.onUnbind(intent)
    }
}
