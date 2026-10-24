package com.team27.mama.emergency.sms

import android.app.Activity
import android.app.PendingIntent
import android.content.BroadcastReceiver
import android.content.Context
import android.content.Intent
import android.content.IntentFilter
import android.os.Build
import android.telephony.SmsManager
import androidx.core.content.ContextCompat
import java.util.concurrent.atomic.AtomicBoolean
import java.util.concurrent.atomic.AtomicInteger
import java.util.concurrent.atomic.AtomicReference

/** Sends one single-part SMS; [onResult] runs once, true when the radio accepted it. */
fun interface SmsSender {
    fun send(number: String, text: String, onResult: (Boolean) -> Unit)
}

/**
 * [SmsManager.sendTextMessage] (never multipart) with its sent-status read back
 * through a one-shot, package-scoped broadcast. No status within [timeoutMs],
 * a missing SEND_SMS permission or a radio error all count as a failed send.
 * Android adapter; not unit tested.
 */
class AndroidSmsSender(
    context: Context,
    private val scheduler: SmsScheduler,
    private val timeoutMs: Long = DEFAULT_TIMEOUT_MS,
) : SmsSender {
    private val app = context.applicationContext
    private val requestCodes = AtomicInteger()

    override fun send(number: String, text: String, onResult: (Boolean) -> Unit) {
        val requestCode = requestCodes.incrementAndGet()
        val action = "${app.packageName}.SMS_SENT.$requestCode"
        val done = AtomicBoolean(false)
        val timeout = AtomicReference<Cancellable?>()
        lateinit var receiver: BroadcastReceiver

        fun finish(ok: Boolean) {
            if (!done.compareAndSet(false, true)) return
            timeout.get()?.cancel()
            try {
                app.unregisterReceiver(receiver)
            } catch (e: IllegalArgumentException) {
                // Already unregistered.
            }
            onResult(ok)
        }

        receiver = object : BroadcastReceiver() {
            override fun onReceive(context: Context, intent: Intent) = finish(resultCode == Activity.RESULT_OK)
        }
        ContextCompat.registerReceiver(app, receiver, IntentFilter(action), ContextCompat.RECEIVER_NOT_EXPORTED)
        val sentIntent = PendingIntent.getBroadcast(
            app,
            requestCode,
            Intent(action).setPackage(app.packageName),
            PendingIntent.FLAG_IMMUTABLE or PendingIntent.FLAG_ONE_SHOT,
        )
        timeout.set(scheduler.schedule(timeoutMs) { finish(false) })
        try {
            smsManager().sendTextMessage(number, null, text, sentIntent, null)
        } catch (e: SecurityException) {
            finish(false)
        } catch (e: IllegalArgumentException) {
            finish(false)
        }
    }

    private fun smsManager(): SmsManager =
        if (Build.VERSION.SDK_INT >= Build.VERSION_CODES.S) {
            app.getSystemService(SmsManager::class.java)
        } else {
            @Suppress("DEPRECATION")
            SmsManager.getDefault()
        }

    private companion object {
        const val DEFAULT_TIMEOUT_MS = 60_000L
    }
}
