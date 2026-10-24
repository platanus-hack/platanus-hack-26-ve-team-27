package com.team27.mama.trigger

import android.app.Notification
import android.app.NotificationChannel
import android.app.NotificationManager
import android.content.Context
import com.team27.mama.R

/**
 * The quiet notification both trigger services must show while they run.
 * Low importance (HyperOS folds it into silent notifications) and neutral
 * words only: whoever is next to the person may read it.
 */
internal object ForegroundNotification {
    private const val CHANNEL = "mama_sync"

    fun build(context: Context, text: String): Notification {
        context.getSystemService(NotificationManager::class.java)
            .createNotificationChannel(NotificationChannel(CHANNEL, "Sincronización", NotificationManager.IMPORTANCE_LOW))
        return Notification.Builder(context, CHANNEL)
            .setSmallIcon(R.drawable.ic_mama_mark)
            .setContentTitle(context.getString(R.string.app_name))
            .setContentText(text)
            .setOngoing(true)
            .build()
    }
}
