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
import com.team27.mama.MamaApp
import com.team27.mama.R

/** Keeps the process alive while the relay is on or an emergency needs the mesh. The controller owns the gateway's stop. */
class RelayForegroundService : Service() {
    override fun onBind(intent: Intent?): IBinder? = null

    override fun onStartCommand(intent: Intent?, flags: Int, startId: Int): Int {
        // startForeground comes FIRST: a service started with startForegroundService that is torn down
        // without it kills the app. Permissions are checked after, and a give-up leaves the foreground.
        createChannel()
        val notification = buildNotification()
        val granted = { permission: String ->
            ContextCompat.checkSelfPermission(this, permission) == PackageManager.PERMISSION_GRANTED
        }
        // satisfied() below already implies a location permission, so only the SecurityException
        // fallback ever changes the type.
        val hasLocation = granted(Manifest.permission.ACCESS_FINE_LOCATION) ||
            granted(Manifest.permission.ACCESS_COARSE_LOCATION)
        val full = ServiceInfo.FOREGROUND_SERVICE_TYPE_CONNECTED_DEVICE or
            (if (hasLocation) ServiceInfo.FOREGROUND_SERVICE_TYPE_LOCATION else 0)
        if (!startForegroundOrFalse(notification, full) &&
            !startForegroundOrFalse(notification, ServiceInfo.FOREGROUND_SERVICE_TYPE_CONNECTED_DEVICE)
        ) {
            Log.w(LOG_TAG, "service: startForeground refused")
            // With startId: a newer startForegroundService already queued keeps the service, which then must
            // reach startForeground; a plain stopSelf() would tear it down first and crash the app.
            stopSelf(startId)
            return START_NOT_STICKY
        }
        // A sticky restart after process death: this process's gate has not seen the controller's start yet.
        if (intent == null) gate.onRestarted(relayEnabled())
        if (!gate.onForegrounded()) {
            Log.w(LOG_TAG, "service: stopped before it started")
            return giveUp(startId)
        }
        if (!MeshPermissions.satisfied(Build.VERSION.SDK_INT, granted)) {
            Log.w(LOG_TAG, "service: missing mesh permission")
            return giveUp(startId)
        }
        // No gateway.start() here: MeshController always calls startGateway right after startService,
        // on its background thread (the first start loads the key store, which must stay off main).
        return START_STICKY
    }

    override fun onDestroy() {
        gate.onDestroyed()
        super.onDestroy()
    }

    /** Stops only when [startId] is the latest start (see the refused path in [onStartCommand]). */
    private fun giveUp(startId: Int): Int {
        ServiceCompat.stopForeground(this, ServiceCompat.STOP_FOREGROUND_REMOVE)
        stopSelf(startId)
        return START_NOT_STICKY
    }

    /** The relay switch as this process sees it; false with the offline emergency flag off. */
    private fun relayEnabled(): Boolean = (application as? MamaApp)?.graph?.relaySettings?.enabled?.value == true

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
            description = "Aviso fijo mientras Aló Mamá escucha alertas de teléfonos cercanos."
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
            .setContentTitle("Aló Mamá está atenta a emergencias cercanas")
            .setOngoing(true)
            .setSilent(true)
            .setContentIntent(open)
            .build()
    }

    companion object {
        private const val LOG_TAG = "MamaRelay"
        private const val CHANNEL_ID = "mama_relay"
        private const val NOTIFICATION_ID = 6001

        private val gate = ForegroundGate()

        /**
         * False when the mesh permissions are missing (starting would end in a refused
         * startForeground and a crash) or when Android refuses a background start.
         */
        fun start(context: Context): Boolean {
            val granted = { permission: String ->
                ContextCompat.checkSelfPermission(context, permission) == PackageManager.PERMISSION_GRANTED
            }
            if (!MeshPermissions.satisfied(Build.VERSION.SDK_INT, granted)) {
                Log.w(LOG_TAG, "service: start skipped, missing mesh permission")
                return false
            }
            gate.onStartRequested()
            return try {
                ContextCompat.startForegroundService(context, Intent(context, RelayForegroundService::class.java))
                true
            } catch (e: IllegalStateException) {
                Log.w(LOG_TAG, "service: start not allowed")
                false
            }
        }

        /** Before the service reached the foreground it stops itself instead (see [ForegroundGate]). */
        fun stop(context: Context) {
            if (gate.onStopRequested()) {
                context.stopService(Intent(context, RelayForegroundService::class.java))
            }
        }
    }
}
