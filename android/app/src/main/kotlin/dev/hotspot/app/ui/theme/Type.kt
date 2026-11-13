package dev.hotspot.app.ui.theme

import androidx.compose.material3.Typography
import androidx.compose.ui.text.TextStyle
import androidx.compose.ui.text.font.FontFamily
import androidx.compose.ui.text.font.FontWeight
import androidx.compose.ui.unit.sp

// Few sizes, two weights. Headings are semibold and slightly tight; body text keeps the platform's default spacing.
private fun style(size: Int, line: Int, weight: FontWeight = FontWeight.Normal, spacing: Double = 0.1) =
  TextStyle(
    fontFamily = FontFamily.Default,
    fontWeight = weight,
    fontSize = size.sp,
    lineHeight = line.sp,
    letterSpacing = spacing.sp,
  )

val Typography =
  Typography(
    headlineMedium = style(30, 36, FontWeight.SemiBold, -0.4),
    headlineSmall = style(26, 32, FontWeight.SemiBold, -0.3),
    titleLarge = style(20, 26, FontWeight.SemiBold, 0.0),
    titleMedium = style(16, 22, FontWeight.SemiBold),
    titleSmall = style(14, 20, FontWeight.SemiBold),
    bodyLarge = style(16, 24),
    bodyMedium = style(14, 20),
    bodySmall = style(13, 18),
    labelLarge = style(14, 20, FontWeight.SemiBold),
    labelMedium = style(12, 16, FontWeight.SemiBold, 0.3),
    labelSmall = style(11, 16, FontWeight.Medium, 0.3),
  )
