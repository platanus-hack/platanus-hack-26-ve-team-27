package com.team27.mama.onboarding

import com.team27.mama.emergency.config.RemoteConfigRepository

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
        "Necesitas internet para terminar de configurar MAMÁ. Conéctate a wifi o datos y vuelve a intentarlo."
    OnboardingResult.ServerUnavailable ->
        "No pudimos conectar con MAMÁ. Revisa tu conexión y vuelve a intentarlo."
}

/** Onboarding needs internet: it ends only once the server config is cached. Blocking. */
class OnboardingGate(
    private val network: NetworkStatus,
    private val config: RemoteConfigRepository,
) {
    fun finish(): OnboardingResult = when {
        !network.isOnline() -> OnboardingResult.Offline
        !config.refresh() -> OnboardingResult.ServerUnavailable
        else -> OnboardingResult.Done
    }
}
