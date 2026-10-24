package com.team27.mama.trigger

import kotlinx.coroutines.delay
import kotlinx.coroutines.runBlocking
import org.junit.Assert.assertEquals
import org.junit.Assert.assertTrue
import org.junit.Test

class HedgeTest {

    private fun after(ms: Long, status: Int, body: String = ""): suspend () -> HttpResult = {
        delay(ms)
        HttpResult(status, body, ms)
    }

    @Test
    fun aFastPrimaryNeverStartsTheBackup() = runBlocking {
        var backupRan = false
        val result = hedged(this, 1_500, after(100, 201, "{}")) { backupRan = true; HttpResult(201) }
        assertEquals(HedgeResult("primary", HttpResult(201, "{}", 100)), result)
        assertEquals(false, backupRan)
    }

    @Test
    fun aSlowPrimaryLosesToTheBackup() = runBlocking {
        val start = System.currentTimeMillis()
        val result = hedged(this, 200, after(2_500, 201), after(100, 201, "backup body"))
        assertEquals("backup", result.winner)
        // The winner's body is what the caller parses.
        assertEquals("backup body", result.result.body)
        // Returns at the backup's answer (~300 ms), not after the slow primary.
        assertTrue(System.currentTimeMillis() - start < 1_500)
    }

    @Test
    fun aFailedPrimaryStartsTheBackupAtOnce() = runBlocking {
        val start = System.currentTimeMillis()
        val result = hedged(this, 5_000, after(50, -1), after(50, 201))
        assertEquals("backup", result.winner)
        assertTrue(System.currentTimeMillis() - start < 2_000)
    }

    @Test
    fun aThrowingPrimaryStillLetsTheBackupWin() = runBlocking {
        val result = hedged(this, 5_000, { throw IllegalStateException("boom") }, after(50, 201))
        assertEquals("backup", result.winner)
    }

    @Test
    fun withoutABackupItWaitsForThePrimary() = runBlocking {
        assertEquals(HedgeResult("primary", HttpResult(201, "", 300)), hedged(this, 100, after(300, 201), null))
    }

    @Test
    fun whenBothFailItReturnsThePrimary() = runBlocking {
        assertEquals(HedgeResult("primary", HttpResult(-1, "", 300)), hedged(this, 100, after(300, -1), after(50, 500)))
    }
}
