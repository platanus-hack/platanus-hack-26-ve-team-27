package com.team27.mama.incomingcall

import android.annotation.SuppressLint
import android.app.Notification
import android.app.NotificationChannel
import android.app.NotificationManager
import android.app.PendingIntent
import android.content.Context
import android.content.Intent
import android.media.AudioAttributes
import android.media.RingtoneManager
import android.os.Build
import androidx.core.app.NotificationCompat
import androidx.core.app.NotificationManagerCompat
import androidx.core.app.Person
import androidx.core.graphics.drawable.IconCompat
import com.team27.mama.MainActivity
import com.team27.mama.R
import com.team27.mama.trigger.IncomingCallLauncher
import com.team27.mama.trigger.TriggerLog

/**
 * The trigger's (#16) [IncomingCallLauncher] for #96: Mamá's call arrives as an
 * Android incoming-call notification. CallStyle with Contestar and Rechazar,
 * CATEGORY_CALL, a high-importance channel with the phone's ringtone looping,
 * and a full-screen intent, so a locked phone shows the whole call screen.
 * The ring stops on Contestar, on Rechazar, when the app leaves the ringing
 * screen, or after [RING_TIMEOUT_MS].
 *
 * Android 14+ allows the full-screen intent only with the "Full-screen
 * notifications" special access (USE_FULL_SCREEN_INTENT); without it the call
 * still rings, as a heads-up notification.
 */
class NotificationIncomingCallLauncher(context: Context) : Ringer {
    private val app = context.applicationContext
    private val notifications = NotificationManagerCompat.from(app)

    // areNotificationsEnabled() covers POST_NOTIFICATIONS, which setup (#15) requests.
    @SuppressLint("MissingPermission")
    override fun ring(incidentId: String) {
        val ringing = CallIntent(CallAction.Ring, incidentId)
        if (!notifications.areNotificationsEnabled()) {
            // Nothing would ring: open the ringing screen instead.
            TriggerLog.log(app, "call: notifications off, opening ${ringing.route?.path} for $incidentId")
            openScreen(ringing)
            return
        }
        try {
            notifications.notify(NOTIFICATION_ID, build(incidentId))
            TriggerLog.log(app, "call: ringing for $incidentId, full screen allowed=${canUseFullScreen()}")
        } catch (e: RuntimeException) {
            // A SecurityException, or a CallStyle the system refuses: still show the call.
            TriggerLog.log(app, "call: notify failed ${e.javaClass.simpleName}: ${e.message}")
            openScreen(ringing)
        }
    }

    override fun stop() = notifications.cancel(NOTIFICATION_ID)

    private fun build(incidentId: String): Notification {
        ensureChannel()
        val mama = Person.Builder()
            .setName(CallCopy.CALLER)
            .setIcon(IconCompat.createWithResource(app, R.drawable.mama_mark))
            .setImportant(true)
            .build()
        val ringing = activityIntent(CallIntent(CallAction.Ring, incidentId))
        val answer = activityIntent(CallIntent(CallAction.Answer, incidentId))
        val decline = declineIntent(CallIntent(CallAction.Decline, incidentId))
        return NotificationCompat.Builder(app, CHANNEL_ID)
            .setSmallIcon(R.drawable.ic_mama_mark)
            .setContentTitle(CallCopy.CALLER)
            .setContentText(CallCopy.TEXT)
            .setStyle(NotificationCompat.CallStyle.forIncomingCall(mama, decline, answer))
            .addPerson(mama)
            .setCategory(NotificationCompat.CATEGORY_CALL)
            .setPriority(NotificationCompat.PRIORITY_MAX)
            .setVisibility(NotificationCompat.VISIBILITY_PUBLIC)
            .setOngoing(true)
            .setContentIntent(ringing)
            .setFullScreenIntent(ringing, true)
            .setTimeoutAfter(RING_TIMEOUT_MS)
            .build()
            // The channel's ringtone loops until the notification goes away.
            .apply { flags = flags or Notification.FLAG_INSISTENT }
    }

    private fun ensureChannel() {
        val ringtone = AudioAttributes.Builder()
            .setUsage(AudioAttributes.USAGE_NOTIFICATION_RINGTONE)
            .setContentType(AudioAttributes.CONTENT_TYPE_SONIFICATION)
            .build()
        val channel = NotificationChannel(CHANNEL_ID, CallCopy.CHANNEL, NotificationManager.IMPORTANCE_HIGH).apply {
            setSound(RingtoneManager.getDefaultUri(RingtoneManager.TYPE_RINGTONE), ringtone)
            enableVibration(true)
            vibrationPattern = longArrayOf(0, 1000, 1000)
            lockscreenVisibility = Notification.VISIBILITY_PUBLIC
        }
        // A no-op once the channel exists: Android keeps the user's settings for it.
        app.getSystemService(NotificationManager::class.java).createNotificationChannel(channel)
    }

    private fun canUseFullScreen(): Boolean =
        Build.VERSION.SDK_INT < 34 || app.getSystemService(NotificationManager::class.java).canUseFullScreenIntent()

    /** MainActivity on the call's screen. CLEAR_TOP re-creates a running MainActivity, so it starts there. */
    private fun screenIntent(call: CallIntent): Intent = Intent(app, MainActivity::class.java)
        .setAction(call.action.action)
        .addFlags(Intent.FLAG_ACTIVITY_NEW_TASK or Intent.FLAG_ACTIVITY_CLEAR_TOP)
        .putCallExtras(call)

    private fun activityIntent(call: CallIntent): PendingIntent =
        PendingIntent.getActivity(app, call.action.ordinal, screenIntent(call), PENDING_FLAGS)

    private fun declineIntent(call: CallIntent): PendingIntent {
        val intent = Intent(app, IncomingCallReceiver::class.java).setAction(call.action.action).putCallExtras(call)
        return PendingIntent.getBroadcast(app, call.action.ordinal, intent, PENDING_FLAGS)
    }

    private fun Intent.putCallExtras(call: CallIntent): Intent = apply {
        call.extras().forEach { (key, value) -> putExtra(key, value) }
    }

    private fun openScreen(call: CallIntent) {
        try {
            app.startActivity(screenIntent(call))
        } catch (e: RuntimeException) {
            TriggerLog.log(app, "call: open failed ${e.javaClass.simpleName}: ${e.message}")
        }
    }

    companion object {
        // A new id when the sound or importance change: Android never updates a created channel.
        private const val CHANNEL_ID = "mama_call_v1"
        private const val NOTIFICATION_ID = 96
        const val RING_TIMEOUT_MS = 30_000L
        private const val PENDING_FLAGS = PendingIntent.FLAG_IMMUTABLE or PendingIntent.FLAG_UPDATE_CURRENT
    }
}
