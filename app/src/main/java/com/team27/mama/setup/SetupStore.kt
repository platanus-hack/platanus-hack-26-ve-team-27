package com.team27.mama.setup

import android.content.Context
import android.content.SharedPreferences
import androidx.core.content.edit
import com.team27.mama.contract.DeviceRegistration
import java.util.UUID

/** The few key-value operations setup needs, so [SetupStore] is unit-tested without Android. */
interface KeyValueStore {
    fun getString(key: String): String?
    fun putString(key: String, value: String?)
    fun getBoolean(key: String): Boolean
    fun putBoolean(key: String, value: Boolean)
}

class SharedPreferencesStore(private val prefs: SharedPreferences) : KeyValueStore {
    override fun getString(key: String): String? = prefs.getString(key, null)

    // commit, not apply: the device ID must be on disk before it reaches the server.
    override fun putString(key: String, value: String?) = prefs.edit(commit = true) { putString(key, value) }

    override fun getBoolean(key: String): Boolean = prefs.getBoolean(key, false)

    override fun putBoolean(key: String, value: Boolean) = prefs.edit(commit = true) { putBoolean(key, value) }

    companion object {
        fun forSetup(context: Context) =
            SharedPreferencesStore(context.getSharedPreferences("mama_setup", Context.MODE_PRIVATE))
    }
}

/**
 * What first-run setup saves on the phone: the device ID (made once, at the
 * first read, and never changed), the numbers, the duress phrase, the raw
 * contact ID of "Mamá" and whether setup finished. Later slices read
 * [registration] to re-send DeviceRegistration (the server answers 422 to an
 * alert from an unknown device).
 */
class SetupStore(private val kv: KeyValueStore, private val newId: () -> String = { UUID.randomUUID().toString() }) {

    val deviceId: String
        get() = kv.getString(KEY_DEVICE_ID) ?: newId().also { kv.putString(KEY_DEVICE_ID, it) }

    var ownNumber: String?
        get() = kv.getString(KEY_OWN_NUMBER)
        set(value) = kv.putString(KEY_OWN_NUMBER, value)

    var familyNumbers: List<String>
        // E.164 has no commas, so a comma-joined string is enough.
        get() = kv.getString(KEY_FAMILY_NUMBERS)?.split(',')?.filter(String::isNotEmpty).orEmpty()
        set(value) = kv.putString(KEY_FAMILY_NUMBERS, value.joinToString(","))

    var duressPhrase: String?
        get() = kv.getString(KEY_DURESS_PHRASE)
        set(value) = kv.putString(KEY_DURESS_PHRASE, value)

    /** How Mamá greets the person (contract `user_name`). Null on a phone set up before #108. */
    var userName: String?
        get() = kv.getString(KEY_USER_NAME)
        set(value) = kv.putString(KEY_USER_NAME, value)

    /** The "todo bien" phrase (contract `safe_phrase`). Null on a phone set up before #108. */
    var safePhrase: String?
        get() = kv.getString(KEY_SAFE_PHRASE)
        set(value) = kv.putString(KEY_SAFE_PHRASE, value)

    var mamaRawContactId: Long?
        get() = kv.getString(KEY_MAMA_CONTACT)?.toLongOrNull()
        set(value) = kv.putString(KEY_MAMA_CONTACT, value?.toString())

    var setupCompleted: Boolean
        get() = kv.getBoolean(KEY_COMPLETED)
        set(value) = kv.putBoolean(KEY_COMPLETED, value)

    /** [PermissionFlow.settled], as "STEP:STATUS" pairs; unknown names (an older build) are dropped. */
    var settledSteps: Map<SetupStep, StepStatus>
        get() = kv.getString(KEY_SETTLED_STEPS)?.split(',').orEmpty().mapNotNull { pair ->
            val (step, status) = pair.split(':').takeIf { it.size == 2 } ?: return@mapNotNull null
            val knownStep = SetupStep.entries.firstOrNull { it.name == step } ?: return@mapNotNull null
            val knownStatus = StepStatus.entries.firstOrNull { it.name == status } ?: return@mapNotNull null
            knownStep to knownStatus
        }.toMap()
        set(value) = kv.putString(KEY_SETTLED_STEPS, value.entries.joinToString(",") { "${it.key.name}:${it.value.name}" })

    /** The DeviceRegistration for what is saved, or null while the numbers or phrase are missing. */
    fun registration(appVersion: String?): DeviceRegistration? {
        val own = ownNumber ?: return null
        val phrase = duressPhrase ?: return null
        val family = familyNumbers.takeIf { it.isNotEmpty() } ?: return null
        return DeviceRegistration(
            deviceId = deviceId,
            phoneNumber = own,
            familyNumbers = family,
            duressPhrase = phrase,
            appVersion = appVersion?.takeIf(String::isNotBlank),
            userName = userName?.takeIf(String::isNotBlank),
            safePhrase = safePhrase?.takeIf(String::isNotBlank),
        )
    }

    private companion object {
        const val KEY_DEVICE_ID = "device_id"
        const val KEY_OWN_NUMBER = "own_number"
        const val KEY_FAMILY_NUMBERS = "family_numbers"
        const val KEY_DURESS_PHRASE = "duress_phrase"
        const val KEY_USER_NAME = "user_name"
        const val KEY_SAFE_PHRASE = "safe_phrase"
        const val KEY_MAMA_CONTACT = "mama_raw_contact_id"
        const val KEY_COMPLETED = "setup_completed"
        const val KEY_SETTLED_STEPS = "settled_steps"
    }
}
