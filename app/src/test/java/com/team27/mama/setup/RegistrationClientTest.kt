package com.team27.mama.setup

import com.team27.mama.contract.DeviceRegistration
import java.io.File
import kotlinx.serialization.json.Json
import org.junit.Assert.assertEquals
import org.junit.Assert.assertTrue
import org.junit.Test

class RegistrationClientTest {

    private val sample: File by lazy {
        listOf(
            File("../server/internal/contract/testdata/device_registration.json"),
            File("server/internal/contract/testdata/device_registration.json"),
        ).first { it.isFile }
    }

    @Test
    fun bodyBuiltFromTheFormMatchesTheContractSample() {
        val form = SetupForm(
            ownNumber = "0414 123 4567",
            familyNumbers = listOf("0424 111 2233", "+58 412 555 6677"),
            duressPhrase = "dile a la tía Carmen que llego tarde",
        ).validated()!!
        val registration = DeviceRegistration(
            deviceId = "3f1c9a52-7b1e-4c8a-9a0e-2d6b8f4e1a77",
            phoneNumber = form.ownNumber,
            familyNumbers = form.familyNumbers,
            duressPhrase = form.duressPhrase,
            appVersion = "0.1.0",
            // The setup form gains these in #108; the sample carries them.
            userName = "Mariana",
            safePhrase = "ya llegué a la casa, todo tranquilo",
            // Registration from the setup form is keyless until #59 wires the signer; the sample carries the proof.
            ed25519PublicKey = "Qjj0xMgcOctYzPP1+kmbgiXDnjbvMvg+Cm1vpph2NXo=",
            ed25519Signature = "JMio38RGA+n2az7/Xiiz4y7724rZcyDt7N90vGav0Vq8SwTXFvJSmH21VJeL323EPmedwMiyrm79YVgIjvXlAQ==",
        )
        assertEquals(
            Json.parseToJsonElement(sample.readText()),
            Json.parseToJsonElement(RegistrationClient.registrationBody(registration)),
        )
    }

    @Test
    fun missingAppVersionIsLeftOutNotNull() {
        val body = RegistrationClient.registrationBody(
            DeviceRegistration("id", "+584141234567", listOf("+584241112233"), "una frase cualquiera"),
        )
        assertTrue(body, "app_version" !in body)
    }

    @Test
    fun devicesUrlToleratesATrailingSlash() {
        assertEquals("http://127.0.0.1:8080/devices", RegistrationClient.devicesUrl("http://127.0.0.1:8080"))
        assertEquals("https://mama.onrender.com/devices", RegistrationClient.devicesUrl(" https://mama.onrender.com/ "))
    }

    @Test
    fun onlyA2xxCountsAsRegistered() {
        assertEquals(RegistrationResult.Registered, RegistrationClient.resultFor(204))
        assertTrue(RegistrationClient.resultFor(400) is RegistrationResult.Failed)
        assertTrue(RegistrationClient.resultFor(503) is RegistrationResult.Failed)
    }
}
