// Derived from bitchat-android mesh/BluetoothMeshService.kt and mesh/UnifiedMeshService.kt @ dfc67da (GPL-3.0).
// Upstream: https://github.com/permissionlesstech/bitchat-android commit
// dfc67dad41691128bc6e7425bd69f2f409cb5bbf, files app/src/main/java/com/bitchat/android/mesh/BluetoothMeshService.kt
// (fields, setupDelegates, startServices, stopServices, sendAnnouncementToPeer, sendBroadcastAnnounce) and
// app/src/main/java/com/bitchat/android/mesh/UnifiedMeshService.kt (startAnnouncementScheduler).
// GPL-3.0: see LICENSE and mesh/NOTICE.md.
// MAMÁ changes: BLE only; no Noise/private messages/files/voice/Wi-Fi Aware/bridge; announce before every
// broadcast; public messages handed to a listener. Also: no debug logging, nickname resolver, mesh
// graph, authenticated peer state, TransportBridgeService or MeshServiceHolder; direct peers are
// published to AppStateStore from onPeerListUpdated; the broadcast ANNOUNCE is awaited into the BLE
// send queue before its MESSAGE so the two cannot be reordered. A device without BLE is rejected before
// any component is built, and the connection manager is built first so a failure leaks no loops. Public
// VOICE_FRAME packets are accepted (upstream checks, no playback) so they are relayed.

package com.team27.mama.mesh

import android.bluetooth.BluetoothDevice
import android.bluetooth.BluetoothManager
import android.content.Context
import android.content.pm.PackageManager
import android.util.Log
import com.team27.mama.mesh.crypto.EncryptionService
import com.team27.mama.mesh.mesh.AnnounceHandlingResult
import com.team27.mama.mesh.mesh.BluetoothConnectionManager
import com.team27.mama.mesh.mesh.BluetoothConnectionManagerDelegate
import com.team27.mama.mesh.mesh.DirectLinkAnnouncementPolicy
import com.team27.mama.mesh.mesh.FragmentManager
import com.team27.mama.mesh.mesh.MessageHandler
import com.team27.mama.mesh.mesh.MessageHandlerDelegate
import com.team27.mama.mesh.mesh.PacketProcessor
import com.team27.mama.mesh.mesh.PacketProcessorDelegate
import com.team27.mama.mesh.mesh.PeerInfo
import com.team27.mama.mesh.mesh.PeerManager
import com.team27.mama.mesh.mesh.PeerManagerDelegate
import com.team27.mama.mesh.mesh.PowerManager
import com.team27.mama.mesh.mesh.SecurityManager
import com.team27.mama.mesh.mesh.SecurityManagerDelegate
import com.team27.mama.mesh.mesh.StoreForwardManager
import com.team27.mama.mesh.mesh.StoreForwardManagerDelegate
import com.team27.mama.mesh.model.PeerCapabilities
import com.team27.mama.mesh.model.RequestSyncPacket
import com.team27.mama.mesh.model.RoutedPacket
import com.team27.mama.mesh.protocol.BitchatPacket
import com.team27.mama.mesh.protocol.MessageType
import com.team27.mama.mesh.protocol.SpecialRecipients
import com.team27.mama.mesh.services.AppStateStore
import com.team27.mama.mesh.sync.GossipSyncManager
import com.team27.mama.mesh.util.AppConstants
import kotlinx.coroutines.CoroutineExceptionHandler
import kotlinx.coroutines.CoroutineScope
import kotlinx.coroutines.Dispatchers
import kotlinx.coroutines.Job
import kotlinx.coroutines.SupervisorJob
import kotlinx.coroutines.cancel
import kotlinx.coroutines.currentCoroutineContext
import kotlinx.coroutines.delay
import kotlinx.coroutines.flow.collectLatest
import kotlinx.coroutines.flow.distinctUntilChanged
import kotlinx.coroutines.flow.map
import kotlinx.coroutines.isActive
import kotlinx.coroutines.launch

/** The bitchat BLE mesh coordinator: wires the copied components together for one run. */
internal class BleMeshNode(
    context: Context,
    private val crypto: EncryptionService,
    private val listener: PublicMessageListener
) : MeshNode {

    companion object {
        private const val TAG = "BleMeshNode"
        private val MAX_TTL: UByte = AppConstants.MESSAGE_TTL_HOPS
        private const val PEER_DISCONNECT_GRACE_MS = AppConstants.Mesh.PEER_DISCONNECT_GRACE_MS

        /**
         * Fails before any component exists. Most components start periodic loops when constructed, so a
         * device without BLE must be rejected here or every retried start() would leak those loops.
         */
        internal fun requireBleHardware(hasBleFeature: Boolean, bluetoothService: Any?) {
            check(hasBleFeature) { "Bluetooth LE is not supported on this device" }
            check(bluetoothService is BluetoothManager) { "Bluetooth service is unavailable" }
        }
    }

    init {
        val app = context.applicationContext
        requireBleHardware(
            app.packageManager.hasSystemFeature(PackageManager.FEATURE_BLUETOOTH_LE),
            app.getSystemService(Context.BLUETOOTH_SERVICE)
        )
    }

    override val myPeerId: String = crypto.myPeerID

    private val scope = CoroutineScope(
        Dispatchers.IO + SupervisorJob() +
            CoroutineExceptionHandler { _, e -> Log.e(TAG, "Uncaught mesh coroutine error: ${e.message}", e) }
    )
    private val factory = MeshPacketFactory(crypto)
    // Built first (it needs only fragmentManager): if it still throws, nothing else has started a loop.
    private val fragmentManager = FragmentManager()
    private val connectionManager = try {
        BluetoothConnectionManager(context.applicationContext, myPeerId, fragmentManager)
    } catch (e: Exception) {
        fragmentManager.shutdown()
        throw e
    }
    private val peerManager = PeerManager()
    private val securityManager = SecurityManager(crypto, myPeerId)
    private val storeForwardManager = StoreForwardManager()
    private val messageHandler = MessageHandler(myPeerId)
    private val packetProcessor = PacketProcessor(myPeerId)
    private val powerManager = PowerManager.getInstance(context.applicationContext)
    private val gossipSyncManager = GossipSyncManager(
        myPeerID = myPeerId,
        scope = scope,
        configProvider = object : GossipSyncManager.ConfigProvider {
            override fun seenCapacity(): Int = 500
            override fun gcsMaxBytes(): Int = 400
            override fun gcsTargetFpr(): Double = 0.01
        }
    )

    private val lock = Any()
    private var started = false
    @Volatile private var stopped = false
    private var announcementJob: Job? = null

    init {
        setupDelegates()
        peerManager.isPeerDirectlyConnected = { peerID ->
            connectionManager.addressPeerMap.containsValue(peerID)
        }
    }

    override fun start(): Boolean {
        synchronized(lock) {
            if (started || stopped) return false
            Log.i(TAG, "Starting BLE mesh with peer ID: $myPeerId")
            if (!connectionManager.startServices()) {
                Log.e(TAG, "Failed to start Bluetooth services")
                return false
            }
            started = true
        }
        gossipSyncManager.start()
        startAnnouncementScheduler()
        return true
    }

    override fun stop() {
        val wasStarted: Boolean
        synchronized(lock) {
            if (stopped) return
            stopped = true
            wasStarted = started
        }
        Log.i(TAG, "Stopping BLE mesh")
        AppStateStore.setDirectPeers(emptySet())
        announcementJob?.cancel()
        if (wasStarted) {
            try {
                broadcast(factory.leave(System.currentTimeMillis()))
            } catch (e: Exception) {
                Log.w(TAG, "Failed to send LEAVE: ${e.message}")
            }
        }
        scope.launch {
            delay(200) // Give leave message time to send

            gossipSyncManager.stop()
            connectionManager.stopServices()
            peerManager.shutdown()
            fragmentManager.shutdown()
            securityManager.shutdown()
            storeForwardManager.shutdown()
            messageHandler.shutdown()
            packetProcessor.shutdown()

            scope.cancel()
            Log.i(TAG, "BLE mesh stopped and scope cancelled")
        }
    }

    override fun sendBroadcast(text: String) {
        synchronized(lock) {
            if (!started || stopped) {
                Log.w(TAG, "Ignoring broadcast: mesh node is not running")
                return
            }
        }
        scope.launch {
            try {
                val sequence = factory.broadcastSequence(text, System.currentTimeMillis())
                sequence.forEachIndexed { index, packet ->
                    if (index < sequence.lastIndex) {
                        // Wait until the BLE queue holds this packet, so the next one cannot overtake it.
                        connectionManager.broadcastControlPacketAndAwaitAcceptance(RoutedPacket(packet))
                    } else {
                        broadcast(packet)
                    }
                    try { gossipSyncManager.onPublicPacketSeen(packet) } catch (_: Exception) { }
                }
            } catch (e: Exception) {
                Log.e(TAG, "Failed to send broadcast: ${e.message}")
            }
        }
    }

    private fun broadcast(packet: BitchatPacket): Boolean = broadcastRouted(RoutedPacket(packet))

    private fun broadcastRouted(routed: RoutedPacket): Boolean = connectionManager.broadcastPacket(routed)

    /** Port of upstream sendBroadcastAnnounce. */
    private fun sendBroadcastAnnounce() {
        try {
            val announce = factory.announce(System.currentTimeMillis())
            broadcast(announce)
            try { gossipSyncManager.onPublicPacketSeen(announce) } catch (_: Exception) { }
        } catch (e: Exception) {
            Log.e(TAG, "Failed to send ANNOUNCE: ${e.message}")
        }
    }

    /** Port of upstream sendAnnouncementToPeer: one ANNOUNCE per newly seen peer. */
    private fun sendAnnouncementToPeer(peerID: String) {
        if (peerManager.hasAnnouncedToPeer(peerID)) return
        sendBroadcastAnnounce()
        peerManager.markPeerAsAnnouncedTo(peerID)
    }

    /** Port of UnifiedMeshService.startAnnouncementScheduler. */
    private fun startAnnouncementScheduler() {
        if (announcementJob?.isActive == true) return
        announcementJob = scope.launch {
            powerManager.profile
                .map { profile -> profile.meshAnnouncementIntervalMs to profile.hasDirectPeers }
                .distinctUntilChanged()
                .collectLatest { (intervalMs, hasRecipients) ->
                    if (!hasRecipients) return@collectLatest
                    // Connection-specific paths already send an immediate announce. Begin the
                    // periodic cadence after the configured interval to avoid a transition burst.
                    while (currentCoroutineContext().isActive) {
                        delay(intervalMs)
                        if (powerManager.profile.value.hasDirectPeers) sendBroadcastAnnounce()
                    }
                }
        }
    }

    private fun setupDelegates() {
        peerManager.delegate = object : PeerManagerDelegate {
            override fun onPeerListUpdated(peerIDs: List<String>) {
                if (stopped) return
                val direct = peerManager.getVerifiedPeers().filter { it.value.isDirectConnection }.keys
                AppStateStore.setDirectPeers(direct)
            }

            override fun onPeerRemoved(peerID: String) {
                try { gossipSyncManager.removeAnnouncementForPeer(peerID) } catch (_: Exception) { }
            }
        }

        securityManager.delegate = object : SecurityManagerDelegate {
            override fun getPeerInfo(peerID: String): PeerInfo? = peerManager.getPeerInfo(peerID)
        }

        storeForwardManager.delegate = object : StoreForwardManagerDelegate {
            override fun isFavorite(peerID: String): Boolean = false

            override fun isPeerOnline(peerID: String): Boolean =
                peerManager.getPeerInfo(peerID)?.isConnected == true

            override fun sendPacket(packet: BitchatPacket) {
                broadcast(packet)
            }
        }

        messageHandler.delegate = object : MessageHandlerDelegate {
            override fun removePeer(peerID: String) {
                peerManager.removePeer(peerID)
            }

            override fun getPeerNickname(peerID: String): String? = peerManager.getPeerNickname(peerID)

            override fun getBroadcastRecipient(): ByteArray = SpecialRecipients.BROADCAST

            override fun getPeerInfo(peerID: String): PeerInfo? = peerManager.getPeerInfo(peerID)

            override fun updatePeerInfoFromVerifiedAnnouncement(
                peerID: String,
                nickname: String,
                noisePublicKey: ByteArray,
                signingPublicKey: ByteArray,
                isVerified: Boolean,
                capabilities: PeerCapabilities?
            ): Boolean = peerManager.updatePeerInfoFromVerifiedAnnouncement(
                peerID,
                nickname,
                noisePublicKey,
                signingPublicKey,
                isVerified,
                capabilities
            )

            override fun verifyEd25519Signature(signature: ByteArray, data: ByteArray, publicKey: ByteArray): Boolean =
                crypto.verifyEd25519Signature(signature, data, publicKey)

            override fun onPublicMessageReceived(peerID: String, content: String, packet: BitchatPacket) {
                listener.onPublicMessage(
                    peerID,
                    content,
                    packet.ttl,
                    packet.timestamp.toLong(),
                    peerManager.getPeerInfo(peerID)?.signingPublicKey
                )
            }
        }

        packetProcessor.delegate = object : PacketProcessorDelegate {
            override fun validatePacketSecurity(packet: BitchatPacket, peerID: String): Boolean =
                securityManager.validatePacket(packet, peerID)

            override fun updatePeerLastSeen(peerID: String) {
                peerManager.updatePeerLastSeen(peerID)
            }

            override fun getPeerNickname(peerID: String): String? = peerManager.getPeerNickname(peerID)

            override fun getNetworkSize(): Int = peerManager.getActivePeerCount()

            override fun getBroadcastRecipient(): ByteArray = SpecialRecipients.BROADCAST

            // No Noise: handshakes and encrypted packets addressed to us are ignored.
            override fun handleNoiseHandshake(routed: RoutedPacket): Boolean = false

            override fun handleNoiseEncrypted(routed: RoutedPacket): Boolean = false

            override suspend fun handleAnnounce(routed: RoutedPacket): Boolean {
                val result = messageHandler.handleAnnounceWithResult(routed)
                if (result !is AnnounceHandlingResult.Accepted) return false

                DirectLinkAnnouncementPolicy.observationFor(routed, MAX_TTL)?.let { observation ->
                    if (connectionManager.observePeerIfCurrent(
                            observation.relayAddress,
                            observation.ingressLinkID,
                            observation.peerID
                        )
                    ) {
                        Log.d(TAG, "Observed direct BLE route ${observation.relayAddress} to ${observation.peerID}")
                        try { peerManager.refreshPeerList() } catch (_: Exception) { }
                        try {
                            gossipSyncManager.scheduleInitialSyncToPeer(observation.peerID, 1_000)
                        } catch (_: Exception) { }
                    } else {
                        Log.d(TAG, "Ignoring ANNOUNCE from stale BLE link ${observation.relayAddress}")
                    }
                }
                try { gossipSyncManager.onPublicPacketSeen(routed.packet) } catch (_: Exception) { }
                return true
            }

            override suspend fun handleMessage(routed: RoutedPacket) {
                messageHandler.handleMessage(routed)
                // Track broadcast messages for sync
                try {
                    val pkt = routed.packet
                    val isBroadcast = (pkt.recipientID == null || pkt.recipientID.contentEquals(SpecialRecipients.BROADCAST))
                    if (isBroadcast && pkt.type == MessageType.MESSAGE.value) {
                        gossipSyncManager.onPublicPacketSeen(pkt)
                    }
                } catch (_: Exception) { }
            }

            // Accepted (not played) so the core relays bitchat users' public voice frames.
            override fun handleVoiceFrame(routed: RoutedPacket): Boolean =
                messageHandler.handlePublicVoiceFrame(routed)

            override suspend fun handleLeave(routed: RoutedPacket) {
                messageHandler.handleLeave(routed)
            }

            override fun handleFragment(packet: BitchatPacket): BitchatPacket? {
                // Track broadcast fragments for gossip sync
                try {
                    val isBroadcast = (packet.recipientID == null || packet.recipientID.contentEquals(SpecialRecipients.BROADCAST))
                    if (isBroadcast && packet.type == MessageType.FRAGMENT.value) {
                        gossipSyncManager.onPublicPacketSeen(packet)
                    }
                } catch (_: Exception) { }
                return fragmentManager.handleFragment(packet)
            }

            override fun handleRequestSync(routed: RoutedPacket) {
                // Decode request and respond with missing packets
                val fromPeer = routed.peerID ?: return
                val req = RequestSyncPacket.decode(routed.packet.payload) ?: return
                gossipSyncManager.handleRequestSync(fromPeer, req)
            }

            override fun sendAnnouncementToPeer(peerID: String) {
                this@BleMeshNode.sendAnnouncementToPeer(peerID)
            }

            override fun sendCachedMessages(peerID: String) {
                storeForwardManager.sendCachedMessages(peerID)
            }

            override fun relayPacket(routed: RoutedPacket) {
                broadcastRouted(routed)
            }

            override fun sendToPeer(peerID: String, routed: RoutedPacket): Boolean =
                connectionManager.sendToPeer(peerID, routed)
        }

        connectionManager.delegate = object : BluetoothConnectionManagerDelegate {
            override fun onPacketReceived(
                packet: BitchatPacket,
                peerID: String,
                device: BluetoothDevice?,
                ingressLinkID: String
            ) {
                packetProcessor.processPacket(
                    RoutedPacket(packet, peerID, device?.address, ingressLinkID = ingressLinkID)
                )
            }

            override fun onDeviceConnected(device: BluetoothDevice) {
                // Send initial announcements after services are ready
                scope.launch {
                    Log.i(TAG, "Device connected: ${device.address}")
                    delay(200)
                    sendBroadcastAnnounce()
                }
            }

            override fun onDeviceDisconnected(device: BluetoothDevice, linkID: String?, peerID: String?) {
                Log.i(TAG, "Device disconnected: ${device.address} (peerID: $peerID)")

                // refresh peer list on disconnect.
                try { peerManager.refreshPeerList() } catch (_: Exception) { }

                // ConnectionTracker already removes an observed mapping only when this exact
                // link is still current. Do not remove by reusable address here: this may be a late
                // disconnect callback from a replaced GATT connection.

                // If the peer that used this link does not come back within a short grace
                // period (no other link, no traffic), remove it instead of waiting for the
                // 3-minute stale-peer sweep.
                if (peerID != null) {
                    val deviceAddress = device.address
                    val disconnectedAt = System.currentTimeMillis()
                    scope.launch {
                        delay(PEER_DISCONNECT_GRACE_MS)
                        try {
                            val linkBack =
                                connectionManager.addressPeerMap.containsKey(deviceAddress) ||
                                    connectionManager.addressPeerMap.containsValue(peerID)
                            val lastSeen = peerManager.getPeerInfo(peerID)?.lastSeen ?: 0L
                            val seenAfterDisconnect = lastSeen > disconnectedAt
                            if (!linkBack && !seenAfterDisconnect) {
                                Log.i(TAG, "Peer $peerID did not return after disconnect; removing peer")
                                peerManager.removePeer(peerID)
                            }
                        } catch (e: Exception) {
                            Log.w(TAG, "Disconnect grace check failed for $peerID: ${e.message}")
                        }
                    }
                }
            }

            override fun onRSSIUpdated(deviceAddress: String, rssi: Int) {
                // Find the peer ID for this device address and update RSSI in PeerManager
                connectionManager.addressPeerMap[deviceAddress]?.let { peerID ->
                    peerManager.updatePeerRSSI(peerID, rssi)
                }
            }
        }

        gossipSyncManager.delegate = object : GossipSyncManager.Delegate {
            override fun sendPacket(packet: BitchatPacket) {
                broadcast(packet)
            }

            override fun sendPacketToPeer(peerID: String, packet: BitchatPacket) {
                connectionManager.sendPacketToPeer(peerID, packet)
            }

            override fun signPacketForBroadcast(packet: BitchatPacket): BitchatPacket = factory.sign(packet)
        }
    }
}
