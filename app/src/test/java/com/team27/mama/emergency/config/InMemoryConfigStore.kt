package com.team27.mama.emergency.config

class InMemoryConfigStore(var json: String? = null) : ConfigStore {
    override fun read(): String? = json

    override fun write(json: String) {
        this.json = json
    }
}
