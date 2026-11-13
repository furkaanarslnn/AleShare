package dev.hotspot.app.ui

import androidx.compose.animation.AnimatedVisibility
import androidx.compose.animation.core.RepeatMode
import androidx.compose.animation.core.animateFloat
import androidx.compose.animation.core.infiniteRepeatable
import androidx.compose.animation.core.rememberInfiniteTransition
import androidx.compose.animation.core.tween
import androidx.compose.animation.expandVertically
import androidx.compose.animation.fadeIn
import androidx.compose.animation.fadeOut
import androidx.compose.animation.shrinkVertically
import androidx.compose.foundation.BorderStroke
import androidx.compose.foundation.background
import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Box
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.ColumnScope
import androidx.compose.foundation.layout.PaddingValues
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.RowScope
import androidx.compose.foundation.layout.Spacer
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.height
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.layout.size
import androidx.compose.foundation.layout.width
import androidx.compose.foundation.shape.CircleShape
import androidx.compose.material3.Button
import androidx.compose.material3.ButtonDefaults
import androidx.compose.material3.FilledTonalButton
import androidx.compose.material3.Icon
import androidx.compose.material3.IconButton
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.Surface
import androidx.compose.material3.Text
import androidx.compose.material3.TextButton
import androidx.compose.runtime.Composable
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.draw.alpha
import androidx.compose.ui.graphics.Color
import androidx.compose.ui.graphics.vector.ImageVector
import androidx.compose.ui.text.font.FontFamily
import androidx.compose.ui.text.style.TextOverflow
import androidx.compose.ui.unit.dp
import dev.hotspot.app.Tr
import dev.hotspot.app.ui.theme.HotspotTheme

/** A flat container: white on the grey page, a hairline border, no shadow. */
@Composable
fun Panel(modifier: Modifier = Modifier, content: @Composable ColumnScope.() -> Unit) {
  Surface(
    modifier = modifier.fillMaxWidth(),
    shape = MaterialTheme.shapes.large,
    color = MaterialTheme.colorScheme.surface,
    border = BorderStroke(1.dp, MaterialTheme.colorScheme.outlineVariant),
  ) {
    Column(modifier = Modifier.padding(20.dp), content = content)
  }
}

/** Shows [content] with the gap below it, and takes no room at all while hidden (a spaced Column would still add a gap). */
@Composable
fun Reveal(visible: Boolean, content: @Composable () -> Unit) {
  AnimatedVisibility(
    visible = visible,
    enter = fadeIn(tween(200)) + expandVertically(tween(250)),
    exit = fadeOut(tween(150)) + shrinkVertically(tween(200)),
  ) {
    Box(modifier = Modifier.padding(bottom = 12.dp)) { content() }
  }
}

@Composable
fun PanelTitle(text: String, modifier: Modifier = Modifier) {
  Text(text, modifier = modifier, style = MaterialTheme.typography.titleMedium, color = MaterialTheme.colorScheme.onSurface)
}

@Composable
fun Caption(text: String, modifier: Modifier = Modifier) {
  Text(text, modifier = modifier, style = MaterialTheme.typography.bodySmall, color = MaterialTheme.colorScheme.onSurfaceVariant)
}

@Composable
fun StatusDot(color: Color, pulsing: Boolean = false) {
  val alpha =
    if (pulsing) {
      rememberInfiniteTransition(label = "pulse")
        .animateFloat(0.35f, 1f, infiniteRepeatable(tween(800), RepeatMode.Reverse), label = "alpha")
        .value
    } else {
      1f
    }
  Box(modifier = Modifier.size(8.dp).alpha(alpha).background(color, CircleShape))
}

/** The main action of a panel. Filled by default; [tonal] for the action that undoes it. */
@Composable
fun ActionButton(text: String, onClick: () -> Unit, modifier: Modifier = Modifier, tonal: Boolean = false, enabled: Boolean = true) {
  val shape = MaterialTheme.shapes.medium
  val size = modifier.fillMaxWidth().height(52.dp)
  if (tonal) {
    FilledTonalButton(onClick = onClick, modifier = size, enabled = enabled, shape = shape) { Text(text, style = MaterialTheme.typography.labelLarge) }
  } else {
    Button(onClick = onClick, modifier = size, enabled = enabled, shape = shape) { Text(text, style = MaterialTheme.typography.labelLarge) }
  }
}

@Composable
fun IconAction(icon: ImageVector, description: String, onClick: () -> Unit, modifier: Modifier = Modifier, enabled: Boolean = true) {
  IconButton(onClick = onClick, modifier = modifier.alpha(if (enabled) 1f else 0.38f), enabled = enabled) {
    Icon(icon, contentDescription = description, modifier = Modifier.size(20.dp), tint = MaterialTheme.colorScheme.onSurfaceVariant)
  }
}

/** A label above a value, with room for actions on the right. */
@Composable
fun ValueRow(label: String, value: String, modifier: Modifier = Modifier, mono: Boolean = false, actions: @Composable RowScope.() -> Unit = {}) {
  Row(modifier = modifier.fillMaxWidth(), verticalAlignment = Alignment.CenterVertically) {
    Column(modifier = Modifier.weight(1f)) {
      Caption(label)
      Text(
        value,
        style = MaterialTheme.typography.bodyLarge,
        fontFamily = if (mono) FontFamily.Monospace else FontFamily.Default,
        color = MaterialTheme.colorScheme.onSurface,
        maxLines = 1,
        overflow = TextOverflow.Ellipsis,
      )
    }
    actions()
  }
}

@Composable
fun Metric(icon: ImageVector, label: String, value: String, modifier: Modifier = Modifier) {
  Column(modifier = modifier, verticalArrangement = Arrangement.spacedBy(2.dp)) {
    Row(verticalAlignment = Alignment.CenterVertically, horizontalArrangement = Arrangement.spacedBy(6.dp)) {
      Icon(icon, contentDescription = null, modifier = Modifier.size(14.dp), tint = MaterialTheme.colorScheme.onSurfaceVariant)
      Caption(label)
    }
    Text(value, style = MaterialTheme.typography.titleMedium, color = MaterialTheme.colorScheme.onSurface)
  }
}

enum class Tone { Info, Warning, Error }

/** A short message with, optionally, one way to deal with it. */
@Composable
fun Banner(tone: Tone, icon: ImageVector, text: String, modifier: Modifier = Modifier, actionLabel: String? = null, onAction: () -> Unit = {}) {
  val status = HotspotTheme.status
  val scheme = MaterialTheme.colorScheme
  val (container, content) =
    when (tone) {
      Tone.Info -> scheme.primaryContainer to scheme.onPrimaryContainer
      Tone.Warning -> status.warnContainer to status.onWarnContainer
      Tone.Error -> scheme.errorContainer to scheme.onErrorContainer
    }
  Surface(modifier = modifier.fillMaxWidth(), shape = MaterialTheme.shapes.medium, color = container, contentColor = content) {
    Row(modifier = Modifier.padding(start = 14.dp, top = 12.dp, bottom = 12.dp, end = 6.dp), verticalAlignment = Alignment.CenterVertically) {
      Icon(icon, contentDescription = null, modifier = Modifier.size(20.dp))
      Spacer(Modifier.width(12.dp))
      Text(text, modifier = Modifier.weight(1f), style = MaterialTheme.typography.bodyMedium)
      if (actionLabel != null) {
        TextButton(onClick = onAction, colors = ButtonDefaults.textButtonColors(contentColor = content), contentPadding = PaddingValues(horizontal = 12.dp)) {
          Text(actionLabel, style = MaterialTheme.typography.labelLarge)
        }
      }
    }
  }
}

/** Two or three choices in one pill, the chosen one raised. */
@Composable
fun <T> SegmentedSwitch(options: List<Pair<T, String>>, selected: T, onSelect: (T) -> Unit, modifier: Modifier = Modifier, enabled: Boolean = true) {
  val scheme = MaterialTheme.colorScheme
  Surface(modifier = modifier.fillMaxWidth().alpha(if (enabled) 1f else 0.38f), shape = MaterialTheme.shapes.medium, color = scheme.surfaceVariant) {
    Row(modifier = Modifier.padding(4.dp), horizontalArrangement = Arrangement.spacedBy(4.dp)) {
      options.forEach { (value, label) ->
        val chosen = value == selected
        Surface(
          onClick = { onSelect(value) },
          enabled = enabled,
          modifier = Modifier.weight(1f).height(40.dp),
          shape = MaterialTheme.shapes.small,
          color = if (chosen) scheme.surface else Color.Transparent,
          border = if (chosen) BorderStroke(1.dp, scheme.outlineVariant) else null,
        ) {
          Box(contentAlignment = Alignment.Center) {
            Text(
              label,
              style = MaterialTheme.typography.labelLarge,
              color = if (chosen) scheme.onSurface else scheme.onSurfaceVariant,
            )
          }
        }
      }
    }
  }
}

/** A round badge with the first letter of a name. */
@Composable
fun Avatar(name: String) {
  Box(
    modifier = Modifier.size(40.dp).background(MaterialTheme.colorScheme.primaryContainer, CircleShape),
    contentAlignment = Alignment.Center,
  ) {
    Text(
      name.trim().take(1).uppercase(Tr.locale).ifEmpty { "?" },
      style = MaterialTheme.typography.titleSmall,
      color = MaterialTheme.colorScheme.onPrimaryContainer,
    )
  }
}
