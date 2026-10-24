package com.team27.mama.emergency.sms

import android.content.BroadcastReceiver
import android.content.Context
import android.content.Intent
import android.provider.Telephony
import com.team27.mama.MamaApp

/**
 * Listens for the server's ack SMS (ADR 0004) so the SMS channel stops sending. Declared in the
 * manifest for SMS_RECEIVED, which needs RECEIVE_SMS; without that permission it never fires and
 * only the early stop is lost. A thin shell: [SmsAckHandler] does the parsing and verifying.
 */
class SmsAckReceiver : BroadcastReceiver() {
    override fun onReceive(context: Context, intent: Intent) {
        if (intent.action != Telephony.Sms.Intents.SMS_RECEIVED_ACTION) return
        val graph = (context.applicationContext as? MamaApp)?.graph ?: return
        if (!graph.offlineEmergency) return
        val parts = Telephony.Sms.Intents.getMessagesFromIntent(intent)
            .map { SmsPart(it.originatingAddress, it.messageBody) }
        graph.smsAckHandler.handleParts(parts)
    }
}
