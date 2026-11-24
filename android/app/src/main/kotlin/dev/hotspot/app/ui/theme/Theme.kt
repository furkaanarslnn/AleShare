package dev.hotspot.app.ui.theme

import androidx.compose.foundation.isSystemInDarkTheme
import androidx.compose.foundation.shape.RoundedCornerShape
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.Shapes
import androidx.compose.material3.darkColorScheme
import androidx.compose.material3.lightColorScheme
import androidx.compose.runtime.Composable
import androidx.compose.runtime.CompositionLocalProvider
import androidx.compose.runtime.staticCompositionLocalOf
import androidx.compose.ui.graphics.Color
import androidx.compose.ui.unit.dp

/** Colours with a fixed meaning that Material's scheme has no slot for. */
class StatusColors(
  /** Sharing is on. */
  val ok: Color,
  /** Something needs attention, but nothing is broken. */
  val warn: Color,
  val warnContainer: Color,
  val onWarnContainer: Color,
)

private val LocalStatusColors = staticCompositionLocalOf<StatusColors> { error("HotspotTheme missing") }

object HotspotTheme {
  val status: StatusColors
    @Composable get() = LocalStatusColors.current
}

private val LightColors =
  lightColorScheme(
    primary = primaryLight,
    onPrimary = onPrimaryLight,
    primaryContainer = primaryContainerLight,
    onPrimaryContainer = onPrimaryContainerLight,
    secondary = secondaryLight,
    onSecondary = Color.White,
    secondaryContainer = secondaryContainerLight,
    onSecondaryContainer = onSecondaryContainerLight,
    tertiary = secondaryLight,
    background = backgroundLight,
    onBackground = onSurfaceLight,
    surface = surfaceLight,
    onSurface = onSurfaceLight,
    surfaceVariant = surfaceVariantLight,
    onSurfaceVariant = onSurfaceVariantLight,
    surfaceTint = primaryLight,
    outline = outlineLight,
    outlineVariant = outlineVariantLight,
    error = errorLight,
    onError = Color.White,
    errorContainer = errorContainerLight,
    onErrorContainer = onErrorContainerLight,
    surfaceDim = backgroundLight,
    surfaceBright = surfaceLight,
    surfaceContainerLowest = surfaceLight,
    surfaceContainerLow = surfaceLight,
    surfaceContainer = surfaceLight,
    surfaceContainerHigh = surfaceLight,
    surfaceContainerHighest = surfaceVariantLight,
  )

private val DarkColors =
  darkColorScheme(
    primary = primaryDark,
    onPrimary = onPrimaryDark,
    primaryContainer = primaryContainerDark,
    onPrimaryContainer = onPrimaryContainerDark,
    secondary = secondaryDark,
    onSecondary = Color(0xFF14202A),
    secondaryContainer = secondaryContainerDark,
    onSecondaryContainer = onSecondaryContainerDark,
    tertiary = secondaryDark,
    background = backgroundDark,
    onBackground = onSurfaceDark,
    surface = surfaceDark,
    onSurface = onSurfaceDark,
    surfaceVariant = surfaceVariantDark,
    onSurfaceVariant = onSurfaceVariantDark,
    surfaceTint = primaryDark,
    outline = outlineDark,
    outlineVariant = outlineVariantDark,
    error = errorDark,
    onError = Color(0xFF3B0A0A),
    errorContainer = errorContainerDark,
    onErrorContainer = onErrorContainerDark,
    surfaceDim = backgroundDark,
    surfaceBright = surfaceVariantDark,
    surfaceContainerLowest = backgroundDark,
    surfaceContainerLow = surfaceDark,
    surfaceContainer = surfaceDark,
    surfaceContainerHigh = surfaceVariantDark,
    surfaceContainerHighest = surfaceVariantDark,
  )

private val LightStatus = StatusColors(okLight, warnLight, warnContainerLight, onWarnContainerLight)
private val DarkStatus = StatusColors(okDark, warnDark, warnContainerDark, onWarnContainerDark)

private val AppShapes =
  Shapes(
    extraSmall = RoundedCornerShape(6.dp),
    small = RoundedCornerShape(10.dp),
    medium = RoundedCornerShape(14.dp),
    large = RoundedCornerShape(20.dp),
    extraLarge = RoundedCornerShape(28.dp),
  )

@Composable
fun HotspotTheme(darkTheme: Boolean = isSystemInDarkTheme(), content: @Composable () -> Unit) {
  CompositionLocalProvider(LocalStatusColors provides if (darkTheme) DarkStatus else LightStatus) {
    MaterialTheme(
      colorScheme = if (darkTheme) DarkColors else LightColors,
      typography = Typography,
      shapes = AppShapes,
      content = content,
    )
  }
}
