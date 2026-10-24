package com.team27.mama.setup

import com.team27.mama.contract.DeviceRegistration
import org.junit.Assert.assertEquals
import org.junit.Assert.assertFalse
import org.junit.Assert.assertNull
import org.junit.Assert.assertTrue
import org.junit.Test

class SetupStoreTest {

    private class MemoryStore : KeyValueStore {
        private val values = mutableMapOf<String, Any?>()
        override fun getString(key: String) = values[key] as String?
        override fun putString(key: String, value: String?) {
            values[key] = value
        }
        override fun getBoolean(key: String) = values[key] as Boolean? ?: false
        override fun putBoolean(key: String, value: Boolean) {
            values[key] = value
        }
    }

    @Test
    fun deviceIdIsMadeOnceAndSurvivesANewStore() {
        val kv = MemoryStore()
        var made = 0
        val newId = {
            made++
            "3f1c9a52-7b1e-4c8a-9a0e-2d6b8f4e1a7$made"
        }
        val first = SetupStore(kv, newId).deviceId
        assertEquals(first, SetupStore(kv, newId).deviceId)
        assertEquals(first, SetupStore(kv, newId).deviceId)
        assertEquals(1, made)
    }

    @Test
    fun defaultDeviceIdIsARandomUuid() {
        val id = SetupStore(MemoryStore()).deviceId
        assertTrue(id, Regex("^[0-9a-f]{8}-[0-9a-f]{4}-4[0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}$").matches(id))
    }

    @Test
    fun registrationIsNullUntilEverythingIsSaved() {
        val store = SetupStore(MemoryStore()) { "id" }
        assertNull(store.registration("0.1.0"))
        store.ownNumber = "+584141234567"
        store.duressPhrase = "dile a la tía Carmen que llego tarde"
        assertNull(store.registration("0.1.0"))
        store.familyNumbers = listOf("+584241112233")
        assertEquals("id", store.registration("0.1.0")?.deviceId)
    }

    @Test
    fun registrationCarriesWhatWasSaved() {
        val store = SetupStore(MemoryStore()) { "3f1c9a52-7b1e-4c8a-9a0e-2d6b8f4e1a77" }
        store.ownNumber = "+584141234567"
        store.familyNumbers = listOf("+584241112233", "+584125556677")
        store.duressPhrase = "dile a la tía Carmen que llego tarde"
        store.userName = "Dani"
        store.safePhrase = "voy llegando, ya casi"
        assertEquals(
            DeviceRegistration(
                deviceId = "3f1c9a52-7b1e-4c8a-9a0e-2d6b8f4e1a77",
                phoneNumber = "+584141234567",
                familyNumbers = listOf("+584241112233", "+584125556677"),
                duressPhrase = "dile a la tía Carmen que llego tarde",
                appVersion = "0.1.0",
                userName = "Dani",
                safePhrase = "voy llegando, ya casi",
            ),
            store.registration("0.1.0"),
        )
        assertNull(store.registration(" ")?.appVersion)
    }

    @Test
    fun aPhoneSetUpBeforeTheNameStillRegistersAndKeepsTheServersValues() {
        val store = SetupStore(MemoryStore()) { "id" }
        store.ownNumber = "+584141234567"
        store.familyNumbers = listOf("+584241112233")
        store.duressPhrase = "dile a la tía Carmen que llego tarde"
        val registration = store.registration("0.1.0")!!
        // Left out, the server keeps what it has stored (docs/contract.md).
        assertNull(registration.userName)
        assertNull(registration.safePhrase)
        store.userName = " "
        assertNull(store.registration("0.1.0")!!.userName)
    }

    @Test
    fun setupCompletedAndContactIdPersist() {
        val kv = MemoryStore()
        val store = SetupStore(kv)
        assertFalse(store.setupCompleted)
        assertNull(store.mamaRawContactId)
        store.setupCompleted = true
        store.mamaRawContactId = 42L
        val again = SetupStore(kv)
        assertTrue(again.setupCompleted)
        assertEquals(42L, again.mamaRawContactId)
        assertEquals(emptyList<String>(), again.familyNumbers)
    }

    @Test
    fun settledStepsPersistAndUnknownNamesAreDropped() {
        val kv = MemoryStore()
        assertEquals(emptyMap<SetupStep, StepStatus>(), SetupStore(kv).settledSteps)
        val saved = mapOf(SetupStep.SMS to StepStatus.SKIPPED, SetupStep.AUTOSTART to StepStatus.VISITED)
        SetupStore(kv).settledSteps = saved
        assertEquals(saved, SetupStore(kv).settledSteps)

        kv.putString("settled_steps", "SMS:SKIPPED,TELEPATHY:SKIPPED,BATTERY:LOST,garbage")
        assertEquals(mapOf(SetupStep.SMS to StepStatus.SKIPPED), SetupStore(kv).settledSteps)
    }
}
