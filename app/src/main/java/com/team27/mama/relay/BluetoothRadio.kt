package com.team27.mama.relay

import android.bluetooth.BluetoothAdapter
import android.bluetooth.BluetoothManager
import android.content.BroadcastReceiver
import android.content.Context
import android.content.Intent
import android.content.IntentFilter
import androidx.core.content.ContextCompat
import kotlinx.coroutines.channels.awaitClose
import kotlinx.coroutines.flow.Flow
import kotlinx.coroutines.flow.callbackFlow
import kotlinx.coroutines.flow.distinctUntilChanged

/** Whether the Bluetooth radio is on, for [MeshController]; not unit tested (android.*). */
object BluetoothRadio {
    fun isOn(context: Context): Flow<Boolean> = callbackFlow {
        val app = context.applicationContext
        val receiver = object : BroadcastReceiver() {
            override fun onReceive(c: Context, intent: Intent) {
                when (intent.getIntExtra(BluetoothAdapter.EXTRA_STATE, BluetoothAdapter.ERROR)) {
                    BluetoothAdapter.STATE_ON -> trySend(true)
                    BluetoothAdapter.STATE_TURNING_OFF, BluetoothAdapter.STATE_OFF -> trySend(false)
                }
            }
        }
        ContextCompat.registerReceiver(
            app,
            receiver,
            IntentFilter(BluetoothAdapter.ACTION_STATE_CHANGED),
            ContextCompat.RECEIVER_NOT_EXPORTED,
        )
        trySend(app.getSystemService(BluetoothManager::class.java)?.adapter?.isEnabled == true)
        awaitClose { app.unregisterReceiver(receiver) }
    }.distinctUntilChanged()
}
