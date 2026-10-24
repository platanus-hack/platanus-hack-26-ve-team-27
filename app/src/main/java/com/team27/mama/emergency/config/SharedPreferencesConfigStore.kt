package com.team27.mama.emergency.config

import android.content.Context

/** Thin SharedPreferences adapter; not unit tested (android.*). */
class SharedPreferencesConfigStore(context: Context) : ConfigStore {
    private val prefs = context.applicationContext.getSharedPreferences("mama_remote_config", Context.MODE_PRIVATE)

    override fun read(): String? = prefs.getString("remote_config_json", null)

    override fun write(json: String) {
        prefs.edit().putString("remote_config_json", json).apply()
    }
}
