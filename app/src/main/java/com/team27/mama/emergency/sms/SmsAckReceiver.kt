package com.team27.mama.emergency.sms

import android.content.BroadcastReceiver
import android.content.Context
import android.content.Intent
import android.provider.Telephony
import com.team27.mama.MamaApp

/**
 * Hears the server's ack SMS. Enabled in the manifest only with the
 * OFFLINE_EMERGENCY_ENABLED build flag. Joins multi-part bodies per sender and
 * hands each to [SmsAckFilter]; a verified ack goes to [SmsAckBus]. Never logs
 * bodies or numbers. Android adapter; not unit tested.
 */
class SmsAckReceiver : BroadcastReceiver() {
    override fun onReceive(context: Context, intent: Intent) {
        if (intent.action != Telephony.Sms.Intents.SMS_RECEIVED_ACTION) return
        val graph = (context.applicationContext as? MamaApp)?.graph ?: return
        val filter = graph.smsAckFilter ?: return
        val bus = graph.smsAcks ?: return
        val messages = Telephony.Sms.Intents.getMessagesFromIntent(intent) ?: return
        messages.filterNotNull()
            .groupBy { it.originatingAddress }
            .forEach { (from, parts) ->
                filter.parse(from, parts.joinToString("") { it.messageBody.orEmpty() })?.let(bus::post)
            }
    }
}
