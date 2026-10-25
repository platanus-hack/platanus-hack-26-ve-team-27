package com.team27.mama.trigger

import android.app.Service
import android.content.Context
import android.content.Intent
import android.content.pm.ServiceInfo
import android.media.AudioAttributes
import android.media.AudioFormat
import android.media.AudioTrack
import android.media.VolumeProvider
import android.media.session.MediaSession
import android.media.session.PlaybackState
import android.os.Handler
import android.os.IBinder
import android.os.Looper
import androidx.core.app.ServiceCompat

/**
 * With the panel fully off, Android does not pass volume keys to the user, so
 * the accessibility filter never sees them; it sends them to the active media
 * session instead. This keeps a silent remote-playback session (no audio, no
 * on-screen player) whose VolumeProvider feeds [PressRouter]. While it runs,
 * the volume keys adjust this session, not the phone's media volume.
 */
class VolumeSessionService : Service() {
    private var session: MediaSession? = null
    private var silence: AudioTrack? = null

    override fun onBind(intent: Intent?): IBinder? = null

    override fun onCreate() {
        super.onCreate()
        val notification = ForegroundNotification.build(this, "Activa")
        try {
            ServiceCompat.startForeground(this, NOTIFICATION_ID, notification, ServiceInfo.FOREGROUND_SERVICE_TYPE_MEDIA_PLAYBACK)
        } catch (e: Exception) {
            TriggerLog.log(this, "session startForeground failed ${e.javaClass.simpleName}: ${e.message}")
            stopSelf()
            return
        }
        session = MediaSession(this, "mama-volume").apply {
            // Without a callback, MediaSession drops every VolumeProvider adjustment.
            setCallback(object : MediaSession.Callback() {}, Handler(Looper.getMainLooper()))
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
        silence = playSilence()
        TriggerLog.log(this, "volume session active silence=${silence?.playState}")
    }

    /**
     * Android 16 routes screen-off volume keys only to the session of the app
     * that last really played audio, so this loops one second of zeros
     * (inaudible; no audio focus, so other music keeps playing).
     */
    private fun playSilence(): AudioTrack? = try {
        val rate = 8_000
        AudioTrack.Builder()
            .setAudioAttributes(AudioAttributes.Builder().setUsage(AudioAttributes.USAGE_MEDIA).setContentType(AudioAttributes.CONTENT_TYPE_MUSIC).build())
            .setAudioFormat(AudioFormat.Builder().setEncoding(AudioFormat.ENCODING_PCM_16BIT).setSampleRate(rate).setChannelMask(AudioFormat.CHANNEL_OUT_MONO).build())
            .setTransferMode(AudioTrack.MODE_STATIC)
            .setBufferSizeInBytes(rate * 2)
            .build()
            .apply {
                write(ShortArray(rate), 0, rate)
                setLoopPoints(0, rate, -1)
                play()
            }
    } catch (e: Exception) {
        TriggerLog.log(this, "silence failed ${e.javaClass.simpleName}: ${e.message}")
        null
    }

    override fun onStartCommand(intent: Intent?, flags: Int, startId: Int): Int = START_STICKY

    override fun onDestroy() {
        silence?.run { runCatching { stop() }; release() }
        silence = null
        session?.release()
        session = null
        TriggerLog.log(this, "volume session released")
        super.onDestroy()
    }

    companion object {
        private const val NOTIFICATION_ID = 11

        fun start(context: Context) {
            try {
                context.startForegroundService(Intent(context, VolumeSessionService::class.java))
            } catch (e: Exception) {
                TriggerLog.log(context, "session start failed ${e.javaClass.simpleName}: ${e.message}")
            }
        }

        fun stop(context: Context) {
            context.stopService(Intent(context, VolumeSessionService::class.java))
        }
    }
}
