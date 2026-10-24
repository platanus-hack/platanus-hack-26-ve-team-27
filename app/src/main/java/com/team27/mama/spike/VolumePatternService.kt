package com.team27.mama.spike

import android.accessibilityservice.AccessibilityService
import android.content.Intent
import android.view.KeyEvent
import android.view.accessibility.AccessibilityEvent

/**
 * Feeds volume presses to [PressRouter]. It never consumes a key, so volume
 * and the system shortcuts behave as usual. It only sees keys Android passes
 * to the user (screen on, or the always-on display lit), so on connect it
 * also starts [VolumeSessionService], which gets them with the panel off.
 */
class VolumePatternService : AccessibilityService() {

    override fun onServiceConnected() {
        SpikeLog.log(this, "a11y connected")
        VolumeSessionService.start(this)
    }

    override fun onKeyEvent(event: KeyEvent): Boolean {
        val key = when (event.keyCode) {
            KeyEvent.KEYCODE_VOLUME_UP -> VolumeKey.UP
            KeyEvent.KEYCODE_VOLUME_DOWN -> VolumeKey.DOWN
            else -> return false
        }
        if (event.action == KeyEvent.ACTION_DOWN && event.repeatCount == 0) PressRouter.onPress(this, key, PressSource.ACCESSIBILITY)
        return false
    }

    override fun onAccessibilityEvent(event: AccessibilityEvent?) = Unit

    override fun onInterrupt() = Unit

    override fun onUnbind(intent: Intent?): Boolean {
        SpikeLog.log(this, "a11y unbound")
        VolumeSessionService.stop(this)
        return super.onUnbind(intent)
    }

    override fun onDestroy() {
        VolumeSessionService.stop(this)
        super.onDestroy()
    }
}
