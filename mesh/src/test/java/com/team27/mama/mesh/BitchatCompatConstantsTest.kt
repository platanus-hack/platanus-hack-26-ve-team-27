package com.team27.mama.mesh

import com.team27.mama.mesh.protocol.MessageType
import com.team27.mama.mesh.protocol.SpecialRecipients
import com.team27.mama.mesh.util.AppConstants
import org.junit.Assert.assertArrayEquals
import org.junit.Assert.assertEquals
import org.junit.Test
import java.util.UUID

/** Pins the bitchat wire and mesh parameters MAMÁ relies on for compatibility. */
class BitchatCompatConstantsTest {

    @Test
    fun bitchatParameters_areUnchanged() {
        assertEquals(7, AppConstants.MESSAGE_TTL_HOPS.toInt())
        assertEquals(100, AppConstants.StoreForward.MAX_CACHED_MESSAGES)
        assertEquals(43_200_000L, AppConstants.StoreForward.MESSAGE_CACHE_TIMEOUT_MS)
        assertEquals(512, AppConstants.Fragmentation.FRAGMENT_SIZE_THRESHOLD)
        assertEquals(UUID.fromString("F47B5E2D-4A9E-4C5A-9B3F-8E1D2C3A4B5C"), AppConstants.Mesh.Gatt.SERVICE_UUID)
        assertEquals(0x02, MessageType.MESSAGE.value.toInt())
        assertArrayEquals(ByteArray(8) { 0xFF.toByte() }, SpecialRecipients.BROADCAST)
    }
}
