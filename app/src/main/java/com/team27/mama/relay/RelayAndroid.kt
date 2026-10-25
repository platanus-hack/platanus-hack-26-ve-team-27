package com.team27.mama.relay

import android.content.Context
import androidx.lifecycle.DefaultLifecycleObserver
import androidx.lifecycle.LifecycleOwner
import androidx.lifecycle.ProcessLifecycleOwner
import kotlinx.coroutines.flow.MutableStateFlow
import kotlinx.coroutines.flow.StateFlow
import kotlinx.coroutines.flow.asStateFlow

private const val PREFS = "mama_relay"

/**
 * The relay switch. Read by [MeshController] and #60's settings screen. On until the person turns it
 * off: it only takes effect once setup's nearby-devices permissions are granted (the service start
 * checks [MeshPermissions]), so accepting that permission is what turns the relay on.
 */
class RelaySettings(context: Context) {
    private val prefs = context.applicationContext.getSharedPreferences(PREFS, Context.MODE_PRIVATE)
    private val mutableEnabled = MutableStateFlow(prefs.getBoolean(KEY_ENABLED, true))

    val enabled: StateFlow<Boolean> = mutableEnabled.asStateFlow()

    fun setEnabled(value: Boolean) {
        prefs.edit().putBoolean(KEY_ENABLED, value).apply()
        mutableEnabled.value = value
    }

    private companion object {
        const val KEY_ENABLED = "relay_enabled"
    }
}

/** [RelayStore] over SharedPreferences. */
class SharedPreferencesRelayStore(context: Context) : RelayStore {
    private val prefs = context.applicationContext.getSharedPreferences(PREFS, Context.MODE_PRIVATE)

    override fun load(): String? = prefs.getString(KEY_STATE, null)

    override fun save(json: String) {
        prefs.edit().putString(KEY_STATE, json).apply()
    }

    private companion object {
        const val KEY_STATE = "relay_state_json"
    }
}

/** True while any MAMÁ activity is started. */
object AppVisibility {
    private val mutableVisible = MutableStateFlow(false)
    val visible: StateFlow<Boolean> = mutableVisible.asStateFlow()

    /** Call once, on the main thread. */
    fun install() {
        ProcessLifecycleOwner.get().lifecycle.addObserver(object : DefaultLifecycleObserver {
            override fun onStart(owner: LifecycleOwner) {
                mutableVisible.value = true
            }

            override fun onStop(owner: LifecycleOwner) {
                mutableVisible.value = false
            }
        })
    }
}
