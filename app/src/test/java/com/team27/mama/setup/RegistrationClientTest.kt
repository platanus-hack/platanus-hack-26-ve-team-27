package com.team27.mama.setup

import com.team27.mama.contract.DeviceRegistration
import java.io.File
import kotlinx.serialization.json.Json
import org.junit.Assert.assertEquals
import org.junit.Assert.assertNull
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
            userName = "Mariana",
            ownNumber = "0414 123 4567",
            familyNumbers = listOf("0424 111 2233", "+58 412 555 6677"),
            duressPhrase = "dile a la tía Carmen que llego tarde",
            safePhrase = "ya llegué a la casa, todo tranquilo",
        ).validated()!!
        val registration = DeviceRegistration(
            deviceId = "3f1c9a52-7b1e-4c8a-9a0e-2d6b8f4e1a77",
            phoneNumber = form.ownNumber,
            familyNumbers = form.familyNumbers,
            duressPhrase = form.duressPhrase,
            appVersion = "0.1.0",
            userName = form.userName,
            safePhrase = form.safePhrase,
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
    fun a422OnOneFieldIsShownOnThatField() {
        // The server's own error texts (server/internal/intake/intake.go).
        fun failed(error: String) = RegistrationClient.resultFor(422, "{\"error\": \"$error\"}") as RegistrationResult.Failed
        assertEquals(FormField.USER_NAME, failed("user_name must be 1 to 30 characters").field)
        assertEquals(
            RegistrationResult.Failed(TOO_CLOSE_TO_DURESS, FormField.SAFE_PHRASE),
            failed("safe_phrase is too close to duress_phrase: pick one that does not share most of its key words"),
        )
        assertEquals(FormField.SAFE_PHRASE, failed("safe_phrase needs at least one word").field)
        assertEquals(FormField.SAFE_PHRASE, failed("safe_phrase must be at most 150 characters and 20 words").field)
        assertEquals(FormField.DURESS_PHRASE, failed("duress_phrase must be at most 150 characters and 20 words").field)
    }

    @Test
    fun otherFailuresAreNotTiedToAField() {
        fun field(status: Int, body: String) = (RegistrationClient.resultFor(status, body) as RegistrationResult.Failed).field
        assertNull(field(422, "{\"error\": \"device_id is required\"}"))
        assertNull(field(422, "<html>bad gateway</html>"))
        assertNull(field(422, ""))
        assertNull(field(400, "{\"error\": \"user_name must be 1 to 30 characters\"}"))
    }

    @Test
    fun onlyA2xxCountsAsRegistered() {
        assertEquals(RegistrationResult.Registered, RegistrationClient.resultFor(204))
        assertTrue(RegistrationClient.resultFor(400) is RegistrationResult.Failed)
        assertTrue(RegistrationClient.resultFor(503) is RegistrationResult.Failed)
    }
}
