// MAMÁ stub replacing bitchat's debug UI settings (not copied).
// Upstream: bitchat-android (https://github.com/permissionlesstech/bitchat-android) commit
// dfc67dad41691128bc6e7425bd69f2f409cb5bbf, app/src/main/java/com/bitchat/android/ui/debug/
// DebugSettingsManager.kt and DebugPreferenceManager.kt. GPL-3.0: see LICENSE and mesh/NOTICE.md.
// Same API as the members the copied mesh code calls: toggles stay true, connection limits stay
// at upstream's default of 8, and every logging call is a no-op.

package com.team27.mama.mesh.ui.debug

import com.team27.mama.mesh.protocol.BitchatPacket
import kotlinx.coroutines.flow.MutableStateFlow
import kotlinx.coroutines.flow.StateFlow
import kotlinx.coroutines.flow.asStateFlow
import java.util.Date

class DebugSettingsManager private constructor() {

    companion object {
        private val INSTANCE = DebugSettingsManager()

        fun getInstance(): DebugSettingsManager = INSTANCE
    }

    val bleEnabled: StateFlow<Boolean> = MutableStateFlow(true).asStateFlow()
    val gattServerEnabled: StateFlow<Boolean> = MutableStateFlow(true).asStateFlow()
    val gattClientEnabled: StateFlow<Boolean> = MutableStateFlow(true).asStateFlow()
    val packetRelayEnabled: StateFlow<Boolean> = MutableStateFlow(true).asStateFlow()

    val maxConnectionsOverall: StateFlow<Int> = MutableStateFlow(8).asStateFlow()
    val maxServerConnections: StateFlow<Int> = MutableStateFlow(8).asStateFlow()
    val maxClientConnections: StateFlow<Int> = MutableStateFlow(8).asStateFlow()

    fun addScanResult(scanResult: DebugScanResult) {}

    fun logPeerConnection(peerID: String, nickname: String, deviceID: String, isInbound: Boolean) {}

    fun logPeerDisconnection(peerID: String, nickname: String, deviceID: String) {}

    fun logIncomingPacket(senderPeerID: String, senderNickname: String?, messageType: String, viaDeviceId: String?) {}

    fun logPacketRelay(
        packetType: String,
        originalPeerID: String,
        originalNickname: String?,
        viaDeviceId: String?
    ) {}

    fun logPacketRelayDetailed(
        packetType: String,
        senderPeerID: String?,
        senderNickname: String?,
        fromPeerID: String?,
        fromNickname: String?,
        fromDeviceAddress: String?,
        toPeerID: String?,
        toNickname: String?,
        toDeviceAddress: String?,
        ttl: UByte?,
        isRelay: Boolean = true,
        packetVersion: UByte = 1u,
        routeInfo: String? = null
    ) {}

    fun logIncoming(packet: BitchatPacket, fromPeerID: String, fromNickname: String?, fromDeviceAddress: String?, myPeerID: String) {}

    fun logOutgoing(packetType: String, toPeerID: String?, toNickname: String?, toDeviceAddress: String?, previousHopPeerID: String? = null, packetVersion: UByte = 1u, routeInfo: String? = null) {}
}

data class DebugScanResult(
    val deviceName: String?,
    val deviceAddress: String,
    val rssi: Int,
    val peerID: String?,
    val timestamp: Date = Date()
)

object DebugPreferenceManager {
    fun getBleEnabled(default: Boolean): Boolean = default
}
