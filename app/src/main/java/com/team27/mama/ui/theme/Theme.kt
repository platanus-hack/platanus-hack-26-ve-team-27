package com.team27.mama.ui.theme

import androidx.compose.foundation.isSystemInDarkTheme
import androidx.compose.foundation.shape.RoundedCornerShape
import androidx.compose.material3.ColorScheme
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.Shapes
import androidx.compose.material3.Typography
import androidx.compose.runtime.Composable
import androidx.compose.ui.graphics.Color
import androidx.compose.ui.text.TextStyle
import androidx.compose.ui.text.font.FontFamily
import androidx.compose.ui.unit.dp

/*
 * THE ONE THEME FILE.
 *
 * Every color, font and corner radius the app uses lives here. Screens read
 * them through MaterialTheme.colorScheme / typography / shapes, so changing a
 * value below changes every screen. No other file may define colors or fonts
 * (see app/UI_RULES.md).
 *
 * The palette below is a calm, neutral placeholder. Ticket #8 (visual
 * identity) replaces it.
 */

// ---------------------------------------------------------------- Palette

private val Ink = Color(0xFF1F2A33)
private val Slate = Color(0xFF3E5566)
private val SlateLight = Color(0xFFA9C1D1)
private val SlateContainer = Color(0xFFD5E4EE)
private val Sage = Color(0xFF5B6E5D)
private val SageLight = Color(0xFFC0D3C1)
private val SageContainer = Color(0xFFDCE9DC)
private val SageDeep = Color(0xFF475848)
private val Clay = Color(0xFF7A5C4F)
private val ClayLight = Color(0xFFE6C3B5)
private val ClayContainer = Color(0xFFF6E1D8)
private val Alert = Color(0xFFB3261E)
private val AlertLight = Color(0xFFF2B8B5)
private val AlertContainer = Color(0xFFF9DEDC)
private val Paper = Color(0xFFF8F9FA)
private val PaperVariant = Color(0xFFDEE3E7)
private val Line = Color(0xFF72787E)
private val Night = Color(0xFF111417)
private val NightVariant = Color(0xFF40484E)
private val Mist = Color(0xFFE1E3E5)
private val White = Color(0xFFFFFFFF)
private val Black = Color(0xFF000000)
// Surface ramps (lowest -> highest) used by cards, sheets, menus and bars.
private val PaperLowest = Color(0xFFFFFFFF)
private val PaperLow = Color(0xFFF2F4F6)
private val PaperMid = Color(0xFFECEFF1)
private val PaperHigh = Color(0xFFE6E9EC)
private val PaperHighest = Color(0xFFE0E4E7)
private val PaperDim = Color(0xFFD8DCDF)
private val NightLowest = Color(0xFF0C0F11) // below surface, by design
private val NightLow = Color(0xFF181C1F)
private val NightMid = Color(0xFF1D2226)
private val NightHigh = Color(0xFF272C30)
private val NightHighest = Color(0xFF32373B)
private val NightBright = Color(0xFF363B3F)
private val LineSoft = Color(0xFFC2C8CD)

/*
 * Both schemes use the full ColorScheme constructor, which has no defaults:
 * the compiler refuses a scheme that forgets a role, so no Material baseline
 * color (the default lavender) can leak into a screen.
 */

val MamaLightColors: ColorScheme = ColorScheme(
    primary = Slate,
    onPrimary = White,
    primaryContainer = SlateContainer,
    onPrimaryContainer = Ink,
    secondary = Sage,
    onSecondary = White,
    secondaryContainer = SageContainer,
    onSecondaryContainer = Ink,
    tertiary = Clay,
    onTertiary = White,
    tertiaryContainer = ClayContainer,
    onTertiaryContainer = Ink,
    error = Alert,
    onError = White,
    errorContainer = AlertContainer,
    onErrorContainer = Ink,
    background = Paper,
    onBackground = Ink,
    surface = Paper,
    onSurface = Ink,
    surfaceVariant = PaperVariant,
    onSurfaceVariant = Slate,
    outline = Line,
    outlineVariant = LineSoft,
    surfaceTint = Slate,
    inversePrimary = SlateLight,
    inverseSurface = NightHigh,
    inverseOnSurface = Mist,
    scrim = Black,
    surfaceBright = Paper,
    surfaceDim = PaperDim,
    surfaceContainerLowest = PaperLowest,
    surfaceContainerLow = PaperLow,
    surfaceContainer = PaperMid,
    surfaceContainerHigh = PaperHigh,
    surfaceContainerHighest = PaperHighest,
    primaryFixed = SlateContainer,
    primaryFixedDim = SlateLight,
    onPrimaryFixed = Ink,
    onPrimaryFixedVariant = Slate,
    secondaryFixed = SageContainer,
    secondaryFixedDim = SageLight,
    onSecondaryFixed = Ink,
    onSecondaryFixedVariant = Sage,
    tertiaryFixed = ClayContainer,
    tertiaryFixedDim = ClayLight,
    onTertiaryFixed = Ink,
    onTertiaryFixedVariant = Clay,
)

val MamaDarkColors: ColorScheme = ColorScheme(
    primary = SlateLight,
    onPrimary = Ink,
    primaryContainer = Slate,
    onPrimaryContainer = SlateContainer,
    secondary = SageLight,
    onSecondary = Ink,
    secondaryContainer = SageDeep,
    onSecondaryContainer = SageContainer,
    tertiary = ClayLight,
    onTertiary = Ink,
    tertiaryContainer = Clay,
    onTertiaryContainer = ClayContainer,
    error = AlertLight,
    onError = Ink,
    errorContainer = Alert,
    onErrorContainer = AlertContainer,
    background = Night,
    onBackground = Mist,
    surface = Night,
    onSurface = Mist,
    surfaceVariant = NightVariant,
    onSurfaceVariant = SlateLight,
    outline = Line,
    outlineVariant = NightVariant,
    surfaceTint = SlateLight,
    inversePrimary = Slate,
    inverseSurface = Mist,
    inverseOnSurface = Ink,
    scrim = Black,
    surfaceBright = NightBright,
    surfaceDim = Night,
    surfaceContainerLowest = NightLowest,
    surfaceContainerLow = NightLow,
    surfaceContainer = NightMid,
    surfaceContainerHigh = NightHigh,
    surfaceContainerHighest = NightHighest,
    primaryFixed = SlateContainer,
    primaryFixedDim = SlateLight,
    onPrimaryFixed = Ink,
    onPrimaryFixedVariant = Slate,
    secondaryFixed = SageContainer,
    secondaryFixedDim = SageLight,
    onSecondaryFixed = Ink,
    onSecondaryFixedVariant = Sage,
    tertiaryFixed = ClayContainer,
    tertiaryFixedDim = ClayLight,
    onTertiaryFixed = Ink,
    onTertiaryFixedVariant = Clay,
)

// ------------------------------------------------------------- Typography

/** Swap this one line to change the font of the whole app. */
val MamaFontFamily: FontFamily = FontFamily.Default

private fun Typography.withFont(font: FontFamily): Typography {
    fun TextStyle.f() = copy(fontFamily = font)
    return copy(
        displayLarge = displayLarge.f(),
        displayMedium = displayMedium.f(),
        displaySmall = displaySmall.f(),
        headlineLarge = headlineLarge.f(),
        headlineMedium = headlineMedium.f(),
        headlineSmall = headlineSmall.f(),
        titleLarge = titleLarge.f(),
        titleMedium = titleMedium.f(),
        titleSmall = titleSmall.f(),
        bodyLarge = bodyLarge.f(),
        bodyMedium = bodyMedium.f(),
        bodySmall = bodySmall.f(),
        labelLarge = labelLarge.f(),
        labelMedium = labelMedium.f(),
        labelSmall = labelSmall.f(),
    )
}

val MamaTypography: Typography = Typography().withFont(MamaFontFamily)

// ----------------------------------------------------------------- Shapes

val MamaShapes: Shapes = Shapes(
    extraSmall = RoundedCornerShape(4.dp),
    small = RoundedCornerShape(8.dp),
    medium = RoundedCornerShape(12.dp),
    large = RoundedCornerShape(16.dp),
    extraLarge = RoundedCornerShape(28.dp),
)

// ------------------------------------------------------------------ Theme

/** Wrap every screen in this. Dynamic color is off on purpose: the palette above wins. */
@Composable
fun MamaTheme(
    darkTheme: Boolean = isSystemInDarkTheme(),
    content: @Composable () -> Unit,
) {
    MaterialTheme(
        colorScheme = if (darkTheme) MamaDarkColors else MamaLightColors,
        typography = MamaTypography,
        shapes = MamaShapes,
        content = content,
    )
}
