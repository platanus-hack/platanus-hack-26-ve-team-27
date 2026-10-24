// MAMÁ stub; upstream AppStateStore not copied. Feeds PowerManager's hasDirectPeers.
// Upstream: bitchat-android (https://github.com/permissionlesstech/bitchat-android) commit
// dfc67dad41691128bc6e7425bd69f2f409cb5bbf, app/src/main/java/com/bitchat/android/services/
// AppStateStore.kt. GPL-3.0: see LICENSE and mesh/NOTICE.md.

package com.team27.mama.mesh.services

import kotlinx.coroutines.flow.MutableStateFlow
import kotlinx.coroutines.flow.StateFlow
import kotlinx.coroutines.flow.asStateFlow

object AppStateStore {
    private val _directPeers = MutableStateFlow<Set<String>>(emptySet())
    val directPeers: StateFlow<Set<String>> = _directPeers.asStateFlow()

    fun setDirectPeers(ids: Collection<String>) {
        _directPeers.value = ids.toSet()
    }
}
