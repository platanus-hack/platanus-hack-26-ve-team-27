// Derived from bitchat-android (https://github.com/permissionlesstech/bitchat-android)
// commit dfc67dad41691128bc6e7425bd69f2f409cb5bbf, file app/src/main/java/com/bitchat/android/service/MeshForegroundService.kt.
// GPL-3.0: see LICENSE and mesh/NOTICE.md. MAMÁ changes: rewritten for MAMÁ; keeps only the foreground-service type selection with the connectedDevice fallback; MAMÁ channel, copy and notification; no peer counts, actions or boot start.
package com.team27.mama.relay

import android.Manifest
import android.app.Notification
import android.app.NotificationChannel
import android.app.NotificationManager
import android.app.PendingIntent
import android.app.Service
import android.content.Context
import android.content.Intent
import android.content.pm.PackageManager
import android.content.pm.ServiceInfo
import android.os.Build
import android.os.IBinder
import android.util.Log
import androidx.core.app.NotificationCompat
import androidx.core.app.ServiceCompat
import androidx.core.content.ContextCompat
import com.team27.mama.MainActivity
import com.team27.mama.R
import com.team27.mama.mesh.MeshGateway

/** Keeps the process alive while the relay is on or an emergency needs the mesh. The controller owns the gateway's stop. */
class RelayForegroundService : Service() {
    override fun onBind(intent: Intent?): IBinder? = null

    override fun onStartCommand(intent: Intent?, flags: Int, startId: Int): Int {
        val granted = { permission: String ->
            ContextCompat.checkSelfPermission(this, permission) == PackageManager.PERMISSION_GRANTED
        }
        if (!MeshPermissions.satisfied(Build.VERSION.SDK_INT, granted)) {
            Log.w(LOG_TAG, "service: missing mesh permission")
            return giveUp()
        }
        createChannel()
        val notification = buildNotification()
        val hasLocation = granted(Manifest.permission.ACCESS_FINE_LOCATION) ||
            granted(Manifest.permission.ACCESS_COARSE_LOCATION)
        val full = ServiceInfo.FOREGROUND_SERVICE_TYPE_CONNECTED_DEVICE or
            (if (hasLocation) ServiceInfo.FOREGROUND_SERVICE_TYPE_LOCATION else 0)
        if (!startForegroundOrFalse(notification, full) &&
            !startForegroundOrFalse(notification, ServiceInfo.FOREGROUND_SERVICE_TYPE_CONNECTED_DEVICE)
        ) {
            Log.w(LOG_TAG, "service: startForeground refused")
            return giveUp()
        }
        try {
            MeshGateway.getInstance(this).start()
        } catch (e: Exception) {
            Log.w(LOG_TAG, "service: gateway start failed")
        }
        return START_STICKY
    }

    private fun giveUp(): Int {
        stopSelf()
        return START_NOT_STICKY
    }

    private fun startForegroundOrFalse(notification: Notification, type: Int): Boolean = try {
        ServiceCompat.startForeground(this, NOTIFICATION_ID, notification, type)
        true
    } catch (e: SecurityException) {
        false
    } catch (e: IllegalStateException) {
        false
    }

    private fun createChannel() {
        val channel = NotificationChannel(CHANNEL_ID, "Ayuda cercana", NotificationManager.IMPORTANCE_LOW).apply {
            description = "Aviso fijo mientras MAMÁ escucha alertas de teléfonos cercanos."
        }
        getSystemService(NotificationManager::class.java).createNotificationChannel(channel)
    }

    private fun buildNotification(): Notification {
        val open = PendingIntent.getActivity(
            this,
            0,
            Intent(this, MainActivity::class.java).addFlags(Intent.FLAG_ACTIVITY_SINGLE_TOP),
            PendingIntent.FLAG_IMMUTABLE or PendingIntent.FLAG_UPDATE_CURRENT,
        )
        return NotificationCompat.Builder(this, CHANNEL_ID)
            .setSmallIcon(R.drawable.ic_mama_mark)
            .setContentTitle("MAMÁ está atenta a emergencias cercanas")
            .setOngoing(true)
            .setSilent(true)
            .setContentIntent(open)
            .build()
    }

    companion object {
        private const val LOG_TAG = "MamaRelay"
        private const val CHANNEL_ID = "mama_relay"
        private const val NOTIFICATION_ID = 6001

        /** False when Android refuses a background start; the controller retries on the next app open. */
        fun start(context: Context): Boolean = try {
            ContextCompat.startForegroundService(context, Intent(context, RelayForegroundService::class.java))
            true
        } catch (e: IllegalStateException) {
            Log.w(LOG_TAG, "service: start not allowed")
            false
        }

        fun stop(context: Context) {
            context.stopService(Intent(context, RelayForegroundService::class.java))
        }
    }
}
