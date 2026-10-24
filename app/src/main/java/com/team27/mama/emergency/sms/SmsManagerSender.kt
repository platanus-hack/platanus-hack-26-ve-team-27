package com.team27.mama.emergency.sms

import android.Manifest
import android.app.Activity
import android.app.PendingIntent
import android.content.BroadcastReceiver
import android.content.Context
import android.content.Intent
import android.content.IntentFilter
import android.content.pm.PackageManager
import android.os.Build
import android.telephony.SmsManager
import androidx.core.content.ContextCompat
import java.util.concurrent.atomic.AtomicBoolean
import java.util.concurrent.atomic.AtomicInteger
import kotlin.coroutines.resume
import kotlinx.coroutines.suspendCancellableCoroutine

/**
 * [SmsSender] over [SmsManager]: needs SEND_SMS, no user interaction. Because of that
 * permission the app is distributed by sideload only (ADR 0004).
 */
class SmsManagerSender(context: Context) : SmsSender {
    private val app = context.applicationContext

    override fun canSend(): Boolean =
        ContextCompat.checkSelfPermission(app, Manifest.permission.SEND_SMS) == PackageManager.PERMISSION_GRANTED

    override suspend fun sendText(to: String, text: String): Boolean {
        if (!canSend()) return false
        val manager = smsManager() ?: return false
        // One action per call, so the sent status of a retry is never mistaken for an earlier one.
        val action = "$SENT_ACTION_PREFIX${counter.incrementAndGet()}"
        return suspendCancellableCoroutine { cont ->
            val done = AtomicBoolean(false)
            val receiver = object : BroadcastReceiver() {
                override fun onReceive(context: Context, intent: Intent) {
                    if (done.compareAndSet(false, true)) {
                        app.unregisterReceiver(this)
                        cont.resume(resultCode == Activity.RESULT_OK)
                    }
                }
            }
            ContextCompat.registerReceiver(app, receiver, IntentFilter(action), ContextCompat.RECEIVER_NOT_EXPORTED)
            cont.invokeOnCancellation {
                if (done.compareAndSet(false, true)) app.unregisterReceiver(receiver)
            }
            val sentIntent = PendingIntent.getBroadcast(
                app,
                0,
                Intent(action).setPackage(app.packageName),
                PendingIntent.FLAG_IMMUTABLE or PendingIntent.FLAG_ONE_SHOT,
            )
            try {
                manager.sendTextMessage(to, null, text, sentIntent, null)
            } catch (e: RuntimeException) {
                // SecurityException (permission revoked meanwhile) or IllegalArgumentException (bad number).
                if (done.compareAndSet(false, true)) {
                    app.unregisterReceiver(receiver)
                    cont.resume(false)
                }
            }
        }
    }

    private fun smsManager(): SmsManager? =
        if (Build.VERSION.SDK_INT >= Build.VERSION_CODES.S) {
            app.getSystemService(SmsManager::class.java)
        } else {
            @Suppress("DEPRECATION")
            SmsManager.getDefault()
        }

    private companion object {
        const val SENT_ACTION_PREFIX = "com.team27.mama.SMS_SENT."
        val counter = AtomicInteger()
    }
}
