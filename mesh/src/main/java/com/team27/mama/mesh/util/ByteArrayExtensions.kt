// Copied from bitchat-android (https://github.com/permissionlesstech/bitchat-android)
// commit dfc67dad41691128bc6e7425bd69f2f409cb5bbf, file app/src/main/java/com/bitchat/android/util/ByteArrayExtensions.kt.
// GPL-3.0: see LICENSE and mesh/NOTICE.md. MAMÁ changes: package renamed to com.team27.mama.mesh.

package com.team27.mama.mesh.util

/**
 * Extension function to convert a ByteArray to a hexadecimal string.
 */
fun ByteArray.toHexString(): String {
    return this.joinToString("") { "%02x".format(it) }
}
