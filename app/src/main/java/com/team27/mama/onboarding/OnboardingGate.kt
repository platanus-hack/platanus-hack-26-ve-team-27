package com.team27.mama.onboarding

import com.team27.mama.emergency.config.RemoteConfigRepository
import java.util.concurrent.Executor

fun interface NetworkStatus {
    fun isOnline(): Boolean
}

sealed interface OnboardingResult {
    data object Done : OnboardingResult
    data object Offline : OnboardingResult
    data object ServerUnavailable : OnboardingResult
}

/** Spanish copy for the setup screen; null when there is nothing to show. */
fun OnboardingResult.userMessage(): String? = when (this) {
    OnboardingResult.Done -> null
    OnboardingResult.Offline ->
        "Necesitas internet para terminar de configurar Aló Mamá. Conéctate a wifi o datos y vuelve a intentarlo."
    OnboardingResult.ServerUnavailable ->
        "No pudimos conectar con Aló Mamá. Revisa tu conexión y vuelve a intentarlo."
}

/**
 * Onboarding needs internet only until the server config is cached. A phone
 * that already holds both server keys is done at once, offline included (the
 * airplane-mode demo), and refreshes on [background] when online. A phone
 * that never fetched config must reach the server. Blocking.
 */
class OnboardingGate(
    private val network: NetworkStatus,
    private val config: RemoteConfigRepository,
    private val background: Executor,
) {
    fun finish(): OnboardingResult {
        val cached = config.current()
        if (cached.serverEd25519PublicKey != null && cached.serverX25519PublicKey != null) {
            if (network.isOnline()) background.execute { config.refresh() }
            return OnboardingResult.Done
        }
        return when {
            !network.isOnline() -> OnboardingResult.Offline
            !config.refresh() -> OnboardingResult.ServerUnavailable
            else -> OnboardingResult.Done
        }
    }
}
