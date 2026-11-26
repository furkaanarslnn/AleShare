package dev.hotspot.app.ui

import androidx.compose.animation.animateColorAsState
import androidx.compose.foundation.Canvas
import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Box
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.ExperimentalLayoutApi
import androidx.compose.foundation.layout.FlowRow
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.Spacer
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.height
import androidx.compose.foundation.layout.offset
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.layout.size
import androidx.compose.foundation.layout.width
import androidx.compose.foundation.shape.CircleShape
import androidx.compose.foundation.shape.RoundedCornerShape
import androidx.compose.foundation.background
import androidx.compose.material3.Button
import androidx.compose.material3.FilledTonalButton
import androidx.compose.material3.HorizontalDivider
import androidx.compose.material3.Icon
import androidx.compose.material3.LinearProgressIndicator
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.OutlinedButton
import androidx.compose.material3.Surface
import androidx.compose.material3.Text
import androidx.compose.material3.TextButton
import androidx.compose.runtime.Composable
import androidx.compose.runtime.getValue
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.geometry.Offset
import androidx.compose.ui.graphics.Brush
import androidx.compose.ui.graphics.Color
import androidx.compose.ui.graphics.Path
import androidx.compose.ui.graphics.StrokeCap
import androidx.compose.ui.graphics.StrokeJoin
import androidx.compose.ui.graphics.drawscope.Stroke
import androidx.compose.ui.graphics.vector.ImageVector
import androidx.compose.ui.text.style.TextOverflow
import androidx.compose.ui.unit.dp
import dev.hotspot.app.GatewayUiState
import dev.hotspot.app.Source
import dev.hotspot.app.WifiChange
import dev.hotspot.app.WifiCredentials
import dev.hotspot.app.bitRate
import dev.hotspot.app.ui.theme.HotspotTheme

/** How many seconds of speeds the live graph shows. */
const val RATE_HISTORY = 60

/** "45 sn", "12 dk", "1 sa 5 dk": how long sharing has been on. */
internal fun uptimeLabel(millis: Long): String {
  val minutes = millis.coerceAtLeast(0) / 60_000
  return when {
    millis < 60_000 -> "${millis.coerceAtLeast(0) / 1000} sn"
    minutes < 60 -> "$minutes dk"
    minutes % 60 == 0L -> "${minutes / 60} sa"
    else -> "${minutes / 60} sa ${minutes % 60} dk"
  }
}

/**
 * The one thing the app does, and the one button that does it. While sharing: for how long, to how many devices,
 * from where and on which band, and how fast it goes now with the last minute as a graph ([history], bytes per
 * second down and up, oldest first). While not: what it does, where the internet comes from, and the start.
 */
@OptIn(ExperimentalLayoutApi::class)
@Composable
fun SharePanel(
  state: GatewayUiState,
  blocked: Boolean,
  source: Source,
  sourceProblem: String?,
  onSource: (Source) -> Unit,
  connected: Int,
  history: List<Pair<Long, Long>>,
  onToggle: () -> Unit,
) {
  val status = HotspotTheme.status
  val scheme = MaterialTheme.colorScheme
  val accent by animateColorAsState(
    when {
      state.running -> status.ok
      state.starting -> status.warn
      else -> scheme.onSurfaceVariant
    },
    label = "status",
  )
  Panel {
    Row(verticalAlignment = Alignment.CenterVertically) {
      StatusPill(
        when {
          state.starting -> "Başlatılıyor"
          state.running -> "Açık"
          else -> "Kapalı"
        },
        accent,
        pulsing = state.starting || (state.running && !state.online),
      )
      Spacer(Modifier.weight(1f))
      if (state.running && state.since > 0) {
        Icon(AppIcons.Clock, contentDescription = null, modifier = Modifier.size(14.dp), tint = scheme.onSurfaceVariant)
        Spacer(Modifier.width(5.dp))
        Caption(uptimeLabel(System.currentTimeMillis() - state.since))
      }
    }
    Spacer(Modifier.height(14.dp))
    Text(
      when {
        state.starting -> "Hazırlanıyor"
        state.running -> "Paylaşım açık"
        else -> "İnternetini paylaş"
      },
      style = MaterialTheme.typography.headlineSmall,
      color = scheme.onSurface,
    )
    Spacer(Modifier.height(6.dp))
    when {
      state.running -> {
        FlowRow(horizontalArrangement = Arrangement.spacedBy(6.dp), verticalArrangement = Arrangement.spacedBy(6.dp)) {
          Fact(AppIcons.Phone, if (connected > 0) "$connected cihaz bağlı" else "Bağlı cihaz yok", strong = connected > 0)
          Fact(if (source == Source.Mobile) AppIcons.Signal else AppIcons.Wifi, source.label)
          state.band?.let { Fact(AppIcons.Wifi, it) }
        }
        if (!state.online) {
          Spacer(Modifier.height(10.dp))
          Caption(if (source == Source.Mobile) "Mobil veri bekleniyor." else "Telefonun bir Wi-Fi ağına bağlanması bekleniyor.")
        }
        Spacer(Modifier.height(16.dp))
        val (down, up) = history.lastOrNull() ?: (0L to 0L)
        Row(modifier = Modifier.fillMaxWidth()) {
          Rate(AppIcons.ArrowDown, "İndirme", bitRate(down * 8), scheme.primary, Modifier.weight(1f))
          Rate(AppIcons.ArrowUp, "Yükleme", bitRate(up * 8), status.ok, Modifier.weight(1f))
        }
        Spacer(Modifier.height(10.dp))
        RateGraph(history, down = scheme.primary, up = status.ok, modifier = Modifier.fillMaxWidth().height(56.dp))
      }
      state.starting -> {
        Caption(if (source == Source.Mobile) "Mobil veri ve Wi-Fi hazırlanıyor." else "Telefonun Wi-Fi bağlantısı ve paylaşım ağı hazırlanıyor.")
        Spacer(Modifier.height(14.dp))
        LinearProgressIndicator(modifier = Modifier.fillMaxWidth().height(3.dp), color = status.warn, trackColor = scheme.surfaceVariant)
      }
      else -> {
        Caption(
          if (source == Source.Mobile) "Bilgisayarın ve tabletin bu telefonun mobil internetini kullanır."
          else "Bilgisayarın ve tabletin, bu telefonun bağlı olduğu Wi-Fi'nin internetini kullanır; şifreli DNS ve reklam engeli onlar için de geçerli olur. Mobil veri harcanmaz."
        )
        Spacer(Modifier.height(16.dp))
        Caption("İnternet kaynağı")
        Spacer(Modifier.height(6.dp))
        SegmentedSwitch(
          options = listOf(Source.Mobile to Source.Mobile.label, Source.Wifi to Source.Wifi.label),
          selected = source,
          onSelect = onSource,
          enabled = !blocked,
        )
      }
    }
    if (state.running && state.online && state.clash) {
      Spacer(Modifier.height(14.dp))
      Banner(Tone.Error, AppIcons.Alert, "Telefon başka bir telefonun Wi-Fi Direct ağına bağlı; o ağ AleShare ağıyla aynı adresleri kullandığı için paylaşılamaz. Telefonu başka bir Wi-Fi'ye bağla.")
    } else if (state.running && state.online && state.captive) {
      Spacer(Modifier.height(14.dp))
      Banner(Tone.Warning, AppIcons.Alert, "Telefonun bağlı olduğu Wi-Fi giriş istiyor. Telefonda Wi-Fi giriş sayfasını tamamla; ardından cihazlar internete çıkar.")
    }
    state.error?.let {
      Spacer(Modifier.height(14.dp))
      Banner(Tone.Error, AppIcons.Alert, it)
    }
    if (blocked) {
      Spacer(Modifier.height(14.dp))
      Banner(Tone.Info, AppIcons.Alert, "Bu cihaz başka bir telefona bağlıyken paylaşım yapamaz. Önce Bağlan sekmesinden bağlantıyı durdur.")
    } else if (sourceProblem != null && !state.running && !state.starting) {
      Spacer(Modifier.height(14.dp))
      Banner(Tone.Info, AppIcons.Alert, sourceProblem)
    }
    Spacer(Modifier.height(18.dp))
    // A sharing that runs can always be stopped, even when the SIM card was taken out meanwhile.
    val enabled = !state.starting && (state.running || (!blocked && sourceProblem == null))
    val shape = MaterialTheme.shapes.medium
    val size = Modifier.fillMaxWidth().height(54.dp)
    if (state.running || state.starting) {
      FilledTonalButton(onClick = onToggle, modifier = size, enabled = enabled, shape = shape) {
        Text(if (state.starting) "Başlatılıyor…" else "Durdur", style = MaterialTheme.typography.labelLarge)
      }
    } else {
      Button(onClick = onToggle, modifier = size, enabled = enabled, shape = shape) {
        Icon(AppIcons.Toggle, contentDescription = null, modifier = Modifier.size(20.dp))
        Spacer(Modifier.width(10.dp))
        Text("Paylaşımı başlat", style = MaterialTheme.typography.labelLarge)
      }
    }
  }
}

/** A short fact in a pill: an icon and a few words. */
@Composable
private fun Fact(icon: ImageVector, text: String, strong: Boolean = false) {
  val scheme = MaterialTheme.colorScheme
  Surface(shape = CircleShape, color = if (strong) scheme.primaryContainer else scheme.surfaceVariant) {
    Row(modifier = Modifier.padding(start = 9.dp, end = 11.dp, top = 5.dp, bottom = 5.dp), verticalAlignment = Alignment.CenterVertically, horizontalArrangement = Arrangement.spacedBy(6.dp)) {
      Icon(icon, contentDescription = null, modifier = Modifier.size(14.dp), tint = if (strong) scheme.onPrimaryContainer else scheme.onSurfaceVariant)
      Text(text, style = MaterialTheme.typography.labelMedium, color = if (strong) scheme.onPrimaryContainer else scheme.onSurface, maxLines = 1, overflow = TextOverflow.Ellipsis)
    }
  }
}

/** One direction's speed now, with the colour its line has in the graph. */
@Composable
private fun Rate(icon: ImageVector, label: String, value: String, color: Color, modifier: Modifier = Modifier) {
  Column(modifier = modifier) {
    Row(verticalAlignment = Alignment.CenterVertically, horizontalArrangement = Arrangement.spacedBy(6.dp)) {
      Box(modifier = Modifier.size(8.dp).background(color, CircleShape))
      Icon(icon, contentDescription = null, modifier = Modifier.size(14.dp), tint = MaterialTheme.colorScheme.onSurfaceVariant)
      Caption(label)
    }
    Spacer(Modifier.height(2.dp))
    Text(value, style = MaterialTheme.typography.titleLarge, color = MaterialTheme.colorScheme.onSurface)
  }
}

/**
 * The last minute's speeds: download as a filled line, upload as a line, on one scale (at least 1 Mbit/s, so that
 * a quiet minute does not look busy).
 */
@Composable
private fun RateGraph(history: List<Pair<Long, Long>>, down: Color, up: Color, modifier: Modifier = Modifier) {
  val grid = MaterialTheme.colorScheme.outlineVariant
  Canvas(modifier = modifier) {
    val n = RATE_HISTORY
    val points = history.takeLast(n)
    val top = maxOf(125_000L, points.maxOfOrNull { maxOf(it.first, it.second) } ?: 0L).toFloat() * 1.15f
    val step = size.width / (n - 1)
    val start = (n - points.size) * step
    fun y(v: Long) = size.height - (v / top) * size.height
    // a base line and a middle line, faint
    drawLine(grid, Offset(0f, size.height - 0.5f), Offset(size.width, size.height - 0.5f), strokeWidth = 1.dp.toPx())
    drawLine(grid.copy(alpha = 0.4f), Offset(0f, size.height / 2), Offset(size.width, size.height / 2), strokeWidth = 1.dp.toPx())
    if (points.size < 2) return@Canvas
    // A smooth line: each reading is the control point of a curve between the midpoints around it.
    fun line(pick: (Pair<Long, Long>) -> Long): Path = Path().apply {
      var px = start
      var py = y(pick(points[0]))
      moveTo(px, py)
      for (i in 1 until points.size) {
        val x = start + i * step
        val yy = y(pick(points[i]))
        quadraticTo(px, py, (px + x) / 2, (py + yy) / 2)
        px = x
        py = yy
      }
      lineTo(px, py)
    }
    val downLine = line { it.first }
    val fill = Path().apply {
      addPath(downLine)
      lineTo(start + (points.size - 1) * step, size.height)
      lineTo(start, size.height)
      close()
    }
    drawPath(fill, Brush.verticalGradient(listOf(down.copy(alpha = 0.28f), down.copy(alpha = 0.02f))))
    val stroke = Stroke(width = 2.dp.toPx(), cap = StrokeCap.Round, join = StrokeJoin.Round)
    drawPath(downLine, down, style = stroke)
    drawPath(line { it.second }, up, style = stroke)
  }
}

/** How to join the network the app opened: its name and password, the QR code, and changing them. */
@Composable
fun NetworkPanel(
  wifi: WifiCredentials,
  change: WifiChange?,
  showPassword: Boolean,
  onTogglePassword: () -> Unit,
  onCopy: (label: String, text: String, sensitive: Boolean) -> Unit,
  onShowQr: () -> Unit,
  onChange: () -> Unit,
  onSwitchNow: () -> Unit,
  onCancelChange: () -> Unit,
) {
  val scheme = MaterialTheme.colorScheme
  Panel {
    Row(verticalAlignment = Alignment.CenterVertically) {
      Box(modifier = Modifier.size(36.dp).background(scheme.primaryContainer, RoundedCornerShape(10.dp)), contentAlignment = Alignment.Center) {
        Icon(AppIcons.Wifi, contentDescription = null, modifier = Modifier.size(20.dp), tint = scheme.onPrimaryContainer)
      }
      Spacer(Modifier.width(12.dp))
      Column(modifier = Modifier.weight(1f)) {
        PanelTitle("Wi-Fi ağı")
        Caption("Cihazlar bu ağa bağlanır")
      }
    }
    Spacer(Modifier.height(12.dp))
    Surface(shape = MaterialTheme.shapes.medium, color = scheme.surfaceVariant, modifier = Modifier.fillMaxWidth()) {
      Column(modifier = Modifier.padding(start = 14.dp, end = 4.dp, top = 6.dp, bottom = 6.dp)) {
        ValueRow("Ağ adı", wifi.ssid, mono = true) {
          IconAction(AppIcons.Copy, "Ağ adını kopyala", onClick = { onCopy("Ağ adı", wifi.ssid, false) })
        }
        HorizontalDivider(modifier = Modifier.padding(vertical = 4.dp).padding(end = 10.dp), color = scheme.outlineVariant)
        ValueRow("Parola", if (showPassword) wifi.passphrase else "••••••••••••••••", mono = true) {
          IconAction(if (showPassword) AppIcons.EyeOff else AppIcons.Eye, if (showPassword) "Parolayı gizle" else "Parolayı göster", onClick = onTogglePassword)
          IconAction(AppIcons.Copy, "Parolayı kopyala", onClick = { onCopy("Parola", wifi.passphrase, true) })
        }
      }
    }
    Spacer(Modifier.height(12.dp))
    if (change != null) {
      val waiting = change.waitingFor.filterNot { it in change.learned }
      Banner(
        Tone.Info,
        AppIcons.Wifi,
        when {
          waiting.isEmpty() && change.passwordOnly -> "Ağın parolası değişiyor."
          waiting.isEmpty() -> "Ağın adı ${change.ssid} oluyor."
          else -> (if (change.passwordOnly) "Ağın parolası değişecek." else "Ağın adı ${change.ssid} olacak.") +
            " Cihazlara haber veriliyor: ${change.waitingFor.size - waiting.size}/${change.waitingFor.size} (beklenen: ${waiting.joinToString()})"
        },
      )
      Row(modifier = Modifier.offset(x = (-12).dp)) {
        TextButton(onClick = onSwitchNow) { Text("Şimdi değiştir", style = MaterialTheme.typography.labelLarge) }
        TextButton(onClick = onCancelChange) { Text("Vazgeç", style = MaterialTheme.typography.labelLarge) }
      }
    } else {
      Caption("Bilgisayarı bu ağa bir kez bağlaman yeterli; Windows hatırlar. Telefon ve tabletler QR kodla bağlanır.")
      Spacer(Modifier.height(12.dp))
      Row(horizontalArrangement = Arrangement.spacedBy(10.dp)) {
        FilledTonalButton(onClick = onShowQr, modifier = Modifier.weight(1f).height(44.dp), shape = MaterialTheme.shapes.small) {
          Icon(AppIcons.Qr, contentDescription = null, modifier = Modifier.size(18.dp))
          Spacer(Modifier.width(8.dp))
          Text("QR kodu", style = MaterialTheme.typography.labelLarge)
        }
        OutlinedButton(onClick = onChange, modifier = Modifier.weight(1f).height(44.dp), shape = MaterialTheme.shapes.small) {
          Icon(AppIcons.Edit, contentDescription = null, modifier = Modifier.size(18.dp))
          Spacer(Modifier.width(8.dp))
          Text("Değiştir", style = MaterialTheme.typography.labelLarge)
        }
      }
    }
  }
}
