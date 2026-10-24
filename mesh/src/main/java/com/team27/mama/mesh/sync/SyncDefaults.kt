// Copied from bitchat-android (https://github.com/permissionlesstech/bitchat-android)
// commit dfc67dad41691128bc6e7425bd69f2f409cb5bbf, file app/src/main/java/com/bitchat/android/sync/SyncDefaults.kt.
// GPL-3.0: see LICENSE and mesh/NOTICE.md. MAMÁ changes: package renamed to com.team27.mama.mesh.

package com.team27.mama.mesh.sync

object SyncDefaults {
    // Default values used when debug prefs are unavailable
    const val DEFAULT_FILTER_BYTES: Int = 256
    const val DEFAULT_FPR_PERCENT: Double = 1.0

    // Receiver-side hard cap to avoid DoS (also enforced in RequestSyncPacket)
    const val MAX_ACCEPT_FILTER_BYTES: Int = 1024
}

