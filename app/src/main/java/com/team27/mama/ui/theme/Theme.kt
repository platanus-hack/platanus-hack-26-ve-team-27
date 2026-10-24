package com.team27.mama.ui.theme

import androidx.compose.foundation.Image
import androidx.compose.foundation.isSystemInDarkTheme
import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.size
import androidx.compose.foundation.shape.CircleShape
import androidx.compose.foundation.shape.RoundedCornerShape
import androidx.compose.material3.ColorScheme
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.Shapes
import androidx.compose.material3.Text
import androidx.compose.material3.Typography
import androidx.compose.runtime.Composable
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.draw.clip
import androidx.compose.ui.graphics.Color
import androidx.compose.ui.layout.ContentScale
import androidx.compose.ui.res.painterResource
import androidx.compose.ui.semantics.clearAndSetSemantics
import androidx.compose.ui.semantics.contentDescription
import androidx.compose.ui.semantics.heading
import androidx.compose.ui.text.TextStyle
import androidx.compose.ui.text.font.Font
import androidx.compose.ui.text.font.FontFamily
import androidx.compose.ui.text.font.FontWeight
import androidx.compose.ui.unit.Dp
import androidx.compose.ui.unit.dp
import androidx.compose.ui.unit.em
import androidx.compose.ui.unit.sp
import com.team27.mama.R

/*
 * THE ONE THEME FILE.
 *
 * Every color, the font, the type scale, the corner radii, the spacing scale
 * and the MAMÁ wordmark live here. Screens read them through
 * MaterialTheme.colorScheme / typography / shapes, MamaSpacing and
 * MamaWordmark(), so changing a value below changes every screen. No other
 * file may define colors or fonts (see app/UI_RULES.md).
 *
 * Identity (#8, #89): palette taken from the Platanus Hack 26 Caracas logo
 * (project-logo.png): charcoal #181918 and chartreuse #EFFE00. The mark is
 * Mamá herself (MamaMark). Dark mode is the logo itself (chartreuse on charcoal).
 * Chartreuse is unreadable on white, so light mode uses a deep olive of the
 * same hue for primary and keeps chartreuse as a soft container. Secondary is
 * a warm graphite (the logo's charcoal) and tertiary a muted periwinkle (the
 * chartreuse complement) for calm accents. Red is only for errors: MAMÁ must
 * look like a family app, never like a panic button. Every text/background
 * pair below passes WCAG AA (4.5:1) in both modes.
 */

// ---------------------------------------------------------------- Palette

// Brand: straight from the logo.
private val Charcoal = Color(0xFF181918) // logo background
private val Chartreuse = Color(0xFFEFFE00) // logo yellow
// Primary ramp (chartreuse hue).
private val Olive = Color(0xFF4A5400)
private val OliveDeep = Color(0xFF3C4400)
private val OliveInk = Color(0xFF161A00)
private val LimeSoft = Color(0xFFE9F68C)
private val LimeDim = Color(0xFFCDDA62)
// Secondary ramp (warm graphite).
private val Graphite = Color(0xFF5D5F52)
private val GraphiteDeep = Color(0xFF45483B)
private val GraphiteInk = Color(0xFF1A1C13)
private val GraphiteDark = Color(0xFF2F3126)
private val StoneSoft = Color(0xFFE3E4D3)
private val StoneDim = Color(0xFFC6C8B6)
// Tertiary ramp (periwinkle, the complement of chartreuse).
private val Periwinkle = Color(0xFF4F5B92)
private val PeriwinkleDeep = Color(0xFF374379)
private val PeriwinkleInk = Color(0xFF0B1849)
private val PeriwinkleDark = Color(0xFF1F2C61)
private val PeriwinkleSoft = Color(0xFFDDE1FF)
private val PeriwinkleDim = Color(0xFFB8C4FF)
// Error ramp (errors only, never brand).
private val Alert = Color(0xFFB3261E)
private val AlertDeep = Color(0xFF8C1D18)
private val AlertInk = Color(0xFF410E0B)
private val AlertDark = Color(0xFF601410)
private val AlertSoft = Color(0xFFF9DEDC)
private val AlertDim = Color(0xFFF2B8B5)
// Neutrals (warm, slightly yellow paper).
private val Paper = Color(0xFFFBFBF2)
private val PaperVariant = Color(0xFFE4E4D4)
private val Ink = Color(0xFF1B1C16)
private val InkVariant = Color(0xFF46483B)
private val Line = Color(0xFF76786A)
private val LineSoft = Color(0xFFC7C8B6)
private val LineDark = Color(0xFF919283)
private val Mist = Color(0xFFE5E5DB)
private val White = Color(0xFFFFFFFF)
private val Black = Color(0xFF000000)
// Surface ramps (lowest -> highest) used by cards, sheets, menus and bars.
private val PaperLowest = Color(0xFFFFFFFF)
private val PaperLow = Color(0xFFF5F5EC)
private val PaperMid = Color(0xFFEFEFE6)
private val PaperHigh = Color(0xFFEAEAE0)
private val PaperHighest = Color(0xFFE4E4DA)
private val PaperDim = Color(0xFFDBDBD2)
private val NightLowest = Color(0xFF121312) // below surface, by design
private val NightLow = Color(0xFF1F201D)
private val NightMid = Color(0xFF242521)
private val NightHigh = Color(0xFF2E2F2B)
private val NightHighest = Color(0xFF393A35)
private val NightBright = Color(0xFF3F403A)

/*
 * Hang-up red (#94), only for the Rechazar and Colgar buttons of the in-app
 * call: every phone paints them red, so a red there reads as an ordinary call.
 * The same in both modes (the call screen is always dark). Nothing else may use it.
 */
val ColorScheme.hangUp: Color get() = Alert
val ColorScheme.onHangUp: Color get() = White

/*
 * Both schemes use the full ColorScheme constructor, which has no defaults:
 * the compiler refuses a scheme that forgets a role, so no Material baseline
 * color (the default lavender) can leak into a screen.
 */

val MamaLightColors: ColorScheme = ColorScheme(
    primary = Olive,
    onPrimary = White,
    primaryContainer = LimeSoft,
    onPrimaryContainer = OliveInk,
    secondary = Graphite,
    onSecondary = White,
    secondaryContainer = StoneSoft,
    onSecondaryContainer = GraphiteInk,
    tertiary = Periwinkle,
    onTertiary = White,
    tertiaryContainer = PeriwinkleSoft,
    onTertiaryContainer = PeriwinkleInk,
    error = Alert,
    onError = White,
    errorContainer = AlertSoft,
    onErrorContainer = AlertInk,
    background = Paper,
    onBackground = Ink,
    surface = Paper,
    onSurface = Ink,
    surfaceVariant = PaperVariant,
    onSurfaceVariant = InkVariant,
    outline = Line,
    outlineVariant = LineSoft,
    surfaceTint = Olive,
    inversePrimary = Chartreuse,
    inverseSurface = Charcoal,
    inverseOnSurface = Mist,
    scrim = Black,
    surfaceBright = Paper,
    surfaceDim = PaperDim,
    surfaceContainerLowest = PaperLowest,
    surfaceContainerLow = PaperLow,
    surfaceContainer = PaperMid,
    surfaceContainerHigh = PaperHigh,
    surfaceContainerHighest = PaperHighest,
    primaryFixed = LimeSoft,
    primaryFixedDim = LimeDim,
    onPrimaryFixed = OliveInk,
    onPrimaryFixedVariant = OliveDeep,
    secondaryFixed = StoneSoft,
    secondaryFixedDim = StoneDim,
    onSecondaryFixed = GraphiteInk,
    onSecondaryFixedVariant = GraphiteDeep,
    tertiaryFixed = PeriwinkleSoft,
    tertiaryFixedDim = PeriwinkleDim,
    onTertiaryFixed = PeriwinkleInk,
    onTertiaryFixedVariant = PeriwinkleDeep,
)

val MamaDarkColors: ColorScheme = ColorScheme(
    primary = Chartreuse,
    onPrimary = Charcoal,
    primaryContainer = OliveDeep,
    onPrimaryContainer = LimeSoft,
    secondary = StoneDim,
    onSecondary = GraphiteDark,
    secondaryContainer = GraphiteDeep,
    onSecondaryContainer = StoneSoft,
    tertiary = PeriwinkleDim,
    onTertiary = PeriwinkleDark,
    tertiaryContainer = PeriwinkleDeep,
    onTertiaryContainer = PeriwinkleSoft,
    error = AlertDim,
    onError = AlertDark,
    errorContainer = AlertDeep,
    onErrorContainer = AlertSoft,
    background = Charcoal,
    onBackground = Mist,
    surface = Charcoal,
    onSurface = Mist,
    surfaceVariant = InkVariant,
    onSurfaceVariant = LineSoft,
    outline = LineDark,
    outlineVariant = InkVariant,
    surfaceTint = Chartreuse,
    inversePrimary = Olive,
    inverseSurface = Mist,
    inverseOnSurface = Ink,
    scrim = Black,
    surfaceBright = NightBright,
    surfaceDim = Charcoal,
    surfaceContainerLowest = NightLowest,
    surfaceContainerLow = NightLow,
    surfaceContainer = NightMid,
    surfaceContainerHigh = NightHigh,
    surfaceContainerHighest = NightHighest,
    primaryFixed = LimeSoft,
    primaryFixedDim = LimeDim,
    onPrimaryFixed = OliveInk,
    onPrimaryFixedVariant = OliveDeep,
    secondaryFixed = StoneSoft,
    secondaryFixedDim = StoneDim,
    onSecondaryFixed = GraphiteInk,
    onSecondaryFixedVariant = GraphiteDeep,
    tertiaryFixed = PeriwinkleSoft,
    tertiaryFixedDim = PeriwinkleDim,
    onTertiaryFixed = PeriwinkleInk,
    onTertiaryFixedVariant = PeriwinkleDeep,
)

// ------------------------------------------------------------- Typography

/*
 * Lexend (SIL OFL 1.1, google/fonts; license in assets/licenses/Lexend-OFL.txt).
 * Designed to cut visual stress while reading, which is the job here, and its
 * wide geometric shapes echo the logo's lettering. Bundled (no downloadable
 * fonts: the demo phone may be offline) as three static Latin-subset weights.
 * Swap this one line to change the font of the whole app.
 */
val MamaFontFamily: FontFamily = FontFamily(Font(R.font.lexend_regular, FontWeight.Normal), Font(R.font.lexend_medium, FontWeight.Medium), Font(R.font.lexend_bold, FontWeight.Bold))

private fun style(weight: FontWeight, size: Int, line: Int, tracking: Double = 0.0) = TextStyle(
    fontFamily = MamaFontFamily,
    fontWeight = weight,
    fontSize = size.sp,
    lineHeight = line.sp,
    letterSpacing = tracking.sp,
)

/** The full Material3 type scale, in the three bundled weights. */
val MamaTypography: Typography = Typography(
    displayLarge = style(FontWeight.Bold, 56, 64, -0.5),
    displayMedium = style(FontWeight.Bold, 44, 52),
    displaySmall = style(FontWeight.Bold, 36, 44),
    headlineLarge = style(FontWeight.Bold, 32, 40),
    headlineMedium = style(FontWeight.Bold, 28, 36),
    headlineSmall = style(FontWeight.Medium, 24, 32),
    titleLarge = style(FontWeight.Medium, 22, 28),
    titleMedium = style(FontWeight.Medium, 16, 24, 0.1),
    titleSmall = style(FontWeight.Medium, 14, 20, 0.1),
    bodyLarge = style(FontWeight.Normal, 16, 24, 0.2),
    bodyMedium = style(FontWeight.Normal, 14, 20, 0.2),
    bodySmall = style(FontWeight.Normal, 12, 16, 0.3),
    labelLarge = style(FontWeight.Medium, 14, 20, 0.1),
    labelMedium = style(FontWeight.Medium, 12, 16, 0.4),
    labelSmall = style(FontWeight.Medium, 11, 16, 0.5),
)

// ----------------------------------------------------------------- Shapes

/** Soft, generous corners: friendly, not clinical. */
val MamaShapes: Shapes = Shapes(
    extraSmall = RoundedCornerShape(8.dp),
    small = RoundedCornerShape(12.dp),
    medium = RoundedCornerShape(16.dp),
    large = RoundedCornerShape(24.dp),
    extraLarge = RoundedCornerShape(32.dp),
)

// ---------------------------------------------------------------- Spacing

/** The spacing scale (4 dp grid). Use these for padding and gaps in screens. */
object MamaSpacing {
    val xs: Dp = 4.dp
    val s: Dp = 8.dp
    val m: Dp = 16.dp
    val l: Dp = 24.dp
    val xl: Dp = 32.dp
    val xxl: Dp = 48.dp

    /** Side margin of every screen. */
    val screen: Dp = 24.dp
}

// --------------------------------------------------------------- Wordmark

/**
 * The MAMÁ wordmark: Mamá's face in a circle, then "MAMÁ". The letters take
 * onSurface and MamaTypography, so they follow every palette or font change in
 * this file; the mark is the illustration itself (#89). [compact] is the
 * top-bar size.
 */
@Composable
fun MamaWordmark(modifier: Modifier = Modifier, compact: Boolean = false) {
    val tile = if (compact) 32.dp else 48.dp
    val text = if (compact) MaterialTheme.typography.titleLarge else MaterialTheme.typography.headlineLarge
    Row(
        modifier = modifier.clearAndSetSemantics {
            contentDescription = "MAMÁ"
            heading()
        },
        verticalAlignment = Alignment.CenterVertically,
        horizontalArrangement = Arrangement.spacedBy(if (compact) MamaSpacing.s else 12.dp),
    ) {
        MamaMark(size = tile)
        Text(
            text = "MAMÁ",
            style = text.copy(fontWeight = FontWeight.Bold, letterSpacing = 0.04.em),
            color = MaterialTheme.colorScheme.onSurface,
        )
    }
}

/**
 * The mark alone: Mamá's face, a head crop of the riso illustration that
 * MamaCharacter animates (res/drawable-nodpi/mama_mark.webp), clipped to a
 * circle. The drawable carries its own chartreuse circle, so it needs no tile
 * color and looks the same in both modes. The launcher icon is the whole bust
 * of the same drawing, as a bitmap foreground per density (ic_launcher_foreground.png
 * under each mipmap folder).
 */
@Composable
fun MamaMark(size: Dp, modifier: Modifier = Modifier) {
    Image(
        painter = painterResource(R.drawable.mama_mark),
        contentDescription = null,
        contentScale = ContentScale.Crop,
        modifier = modifier.size(size).clip(CircleShape),
    )
}

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
