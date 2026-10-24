package com.team27.mama.spike

import android.app.Notification
import android.app.NotificationChannel
import android.app.NotificationManager
import android.app.Service
import android.content.Context
import android.content.Intent
import android.content.pm.ServiceInfo
import android.media.VolumeProvider
import android.media.session.MediaSession
import android.media.session.PlaybackState
import android.os.IBinder
import androidx.core.app.ServiceCompat
import com.team27.mama.R

/**
 * With the panel fully off, Android does not pass volume keys to the user, so
 * the accessibility filter never sees them; it sends them to the active media
 * session instead. This keeps a silent remote-playback session (no audio, no
 * on-screen player) whose VolumeProvider feeds [PressRouter]. While it runs,
 * the volume keys adjust this session, not the phone's media volume.
 */
class VolumeSessionService : Service() {
    private var session: MediaSession? = null

    override fun onBind(intent: Intent?): IBinder? = null

    override fun onCreate() {
        super.onCreate()
        val manager = getSystemService(NotificationManager::class.java)
        manager.createNotificationChannel(NotificationChannel(CHANNEL, "Sincronización", NotificationManager.IMPORTANCE_LOW))
        val notification = Notification.Builder(this, CHANNEL)
            .setSmallIcon(R.drawable.ic_mama_mark)
            .setContentTitle(getString(R.string.app_name))
            .setContentText("Activa")
            .setOngoing(true)
            .build()
        try {
            ServiceCompat.startForeground(this, NOTIFICATION_ID, notification, ServiceInfo.FOREGROUND_SERVICE_TYPE_MEDIA_PLAYBACK)
        } catch (e: Exception) {
            SpikeLog.log(this, "session startForeground failed ${e.javaClass.simpleName}: ${e.message}")
        }
        session = MediaSession(this, "mama-volume").apply {
            setPlaybackToRemote(object : VolumeProvider(VOLUME_CONTROL_RELATIVE, 100, 50) {
                override fun onAdjustVolume(direction: Int) {
                    val key = when {
                        direction > 0 -> VolumeKey.UP
                        direction < 0 -> VolumeKey.DOWN
                        else -> return
                    }
                    PressRouter.onPress(this@VolumeSessionService, key, PressSource.MEDIA_SESSION)
                }
            })
            setPlaybackState(PlaybackState.Builder().setState(PlaybackState.STATE_PLAYING, 0, 1f).build())
            isActive = true
        }
        SpikeLog.log(this, "volume session active")
    }

    override fun onStartCommand(intent: Intent?, flags: Int, startId: Int): Int = START_STICKY

    override fun onDestroy() {
        session?.release()
        session = null
        SpikeLog.log(this, "volume session released")
        super.onDestroy()
    }

    companion object {
        private const val CHANNEL = "mama_sync"
        private const val NOTIFICATION_ID = 11

        fun start(context: Context) {
            try {
                context.startForegroundService(Intent(context, VolumeSessionService::class.java))
            } catch (e: Exception) {
                SpikeLog.log(context, "session start failed ${e.javaClass.simpleName}: ${e.message}")
            }
        }
    }
}
