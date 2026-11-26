package dev.hotspot.app.ui

import androidx.compose.foundation.BorderStroke
import androidx.compose.foundation.background
import androidx.compose.foundation.border
import androidx.compose.foundation.clickable
import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Box
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.ExperimentalLayoutApi
import androidx.compose.foundation.layout.FlowRow
import androidx.compose.foundation.layout.PaddingValues
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.Spacer
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.height
import androidx.compose.foundation.layout.heightIn
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.layout.size
import androidx.compose.foundation.layout.width
import androidx.compose.foundation.rememberScrollState
import androidx.compose.foundation.shape.CircleShape
import androidx.compose.foundation.shape.RoundedCornerShape
import androidx.compose.foundation.verticalScroll
import androidx.compose.material3.ButtonDefaults
import androidx.compose.material3.ExperimentalMaterial3Api
import androidx.compose.material3.FilledTonalButton
import androidx.compose.material3.HorizontalDivider
import androidx.compose.material3.Icon
import androidx.compose.material3.LinearProgressIndicator
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.ModalBottomSheet
import androidx.compose.material3.OutlinedButton
import androidx.compose.material3.Surface
import androidx.compose.material3.Switch
import androidx.compose.material3.Text
import androidx.compose.material3.rememberModalBottomSheetState
import androidx.compose.runtime.Composable
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.remember
import androidx.compose.runtime.setValue
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.graphics.Color
import androidx.compose.ui.semantics.Role
import androidx.compose.ui.text.style.TextOverflow
import androidx.compose.ui.unit.Dp
import androidx.compose.ui.unit.dp
import dev.hotspot.app.Controls
import dev.hotspot.app.QuietHours
import dev.hotspot.app.bitRate
import dev.hotspot.app.humanBytes
import dev.hotspot.app.limitLabel
import dev.hotspot.app.ui.theme.HotspotTheme

/** What the devices list and a device's page show about one paired device. */
data class DeviceView(
  val name: String,
  /** Paired on this phone's screen with its own key (a Windows computer); otherwise a phone, tablet or guest. */
  val computer: Boolean,
  /** Its tunnel carried something a moment ago. */
  val connected: Boolean,
  val pause: Controls.Pause,
  val usedToday: Long,
  /** Daily limit in MiB; 0 is none. */
  val limitMb: Long,
  /** Speed limit in kilobits per second; 0 is none. */
  val speedKbps: Long,
  val quiet: QuietHours?,
  /** What is left of a guest's time or data, for a guest. */
  val guest: String?,
)

/** How a device is doing, in a few words, and whether that wants attention. */
internal data class DeviceState(val text: String, val kind: Kind) {
  enum class Kind { Connected, Idle, Held }
}

internal fun deviceState(d: DeviceView, sharing: Boolean): DeviceState = when (d.pause) {
  Controls.Pause.Manual -> DeviceState("Duraklatıldı", DeviceState.Kind.Held)
  Controls.Pause.Limit -> DeviceState("Günlük sınır doldu", DeviceState.Kind.Held)
  Controls.Pause.Quiet -> DeviceState(d.quiet?.let { "İnternet kapalı · ${QuietHours.clockAt(it.to)} açılır" } ?: "İnternet kapalı", DeviceState.Kind.Held)
  Controls.Pause.None -> when {
    sharing && d.connected -> DeviceState("Bağlı", DeviceState.Kind.Connected)
    else -> DeviceState("Bağlı değil", DeviceState.Kind.Idle)
  }
}

/** The rules a device has, in one short line ("Sınır 1 GB · 5 Mbit/s · 23:00–07:00 kapalı"); "" when none. */
internal fun deviceRules(d: DeviceView): String = listOfNotNull(
  d.guest,
  d.limitMb.takeIf { it > 0 }?.let { "Sınır ${limitLabel(it)}" },
  d.speedKbps.takeIf { it > 0 }?.let { speedLabel(it) },
  d.quiet?.let { "${it.label()} kapalı" },
).joinToString(" · ")

/** "3 cihaz · 2 bağlı". */
internal fun devicesSummary(devices: List<DeviceView>, sharing: Boolean): String {
  val connected = devices.count { sharing && it.connected && it.pause == Controls.Pause.None }
  return "${devices.size} cihaz" + if (sharing) " · $connected bağlı" else ""
}

@Composable
private fun stateColor(kind: DeviceState.Kind): Color = when (kind) {
  DeviceState.Kind.Connected -> HotspotTheme.status.ok
  DeviceState.Kind.Held -> HotspotTheme.status.warn
  DeviceState.Kind.Idle -> MaterialTheme.colorScheme.outline
}

/** The device's kind as a picture, in a rounded tile, with a dot that tells how it is doing. */
@Composable
private fun DeviceTile(d: DeviceView, state: DeviceState, size: Dp = 44.dp) {
  val scheme = MaterialTheme.colorScheme
  Box(modifier = Modifier.size(size + 4.dp)) {
    Box(
      modifier = Modifier.size(size).background(
        if (state.kind == DeviceState.Kind.Idle) scheme.surfaceVariant else scheme.primaryContainer,
        RoundedCornerShape(size / 3.5f),
      ),
      contentAlignment = Alignment.Center,
    ) {
      Icon(
        if (d.computer) AppIcons.Laptop else AppIcons.Phone,
        contentDescription = if (d.computer) "Bilgisayar" else "Telefon ya da tablet",
        modifier = Modifier.size(size * 0.5f),
        tint = if (state.kind == DeviceState.Kind.Idle) scheme.onSurfaceVariant else scheme.onPrimaryContainer,
      )
    }
    Box(
      modifier = Modifier
        .align(Alignment.BottomEnd)
        .size(14.dp)
        .border(2.5.dp, scheme.surface, CircleShape)
        .padding(2.5.dp)
        .background(stateColor(state.kind), CircleShape),
    )
  }
}

/** The paired devices on the Share tab: each with how it is doing and what it used today; a tap opens its page. */
@Composable
fun DevicesPanel(
  devices: List<DeviceView>,
  sharing: Boolean,
  onAdd: () -> Unit,
  onOpen: (String) -> Unit,
  locked: Boolean,
) {
  Panel {
    Row(modifier = Modifier.fillMaxWidth(), verticalAlignment = Alignment.CenterVertically) {
      Column(modifier = Modifier.weight(1f)) {
        PanelTitle("Cihazlar")
        if (devices.isNotEmpty()) Caption(devicesSummary(devices, sharing))
      }
      FilledTonalButton(
        onClick = onAdd,
        enabled = !locked,
        modifier = Modifier.height(36.dp),
        shape = MaterialTheme.shapes.small,
        contentPadding = PaddingValues(start = 12.dp, end = 16.dp),
      ) {
        Icon(AppIcons.Plus, contentDescription = null, modifier = Modifier.size(16.dp))
        Spacer(Modifier.width(6.dp))
        Text("Ekle", style = MaterialTheme.typography.labelLarge)
      }
    }
    if (locked) {
      Spacer(Modifier.height(10.dp))
      Caption("Bu cihaz başka bir telefona bağlıyken cihaz eklenemez ve ayarlar değiştirilemez.")
    } else if (devices.isEmpty()) {
      Spacer(Modifier.height(12.dp))
      EmptyDevices(sharing)
    }
    if (devices.isNotEmpty()) Spacer(Modifier.height(8.dp))
    devices.forEachIndexed { i, d ->
      if (i > 0) HorizontalDivider(modifier = Modifier.padding(start = 62.dp), color = MaterialTheme.colorScheme.outlineVariant)
      DeviceRow(d, sharing, enabled = !locked, onClick = { onOpen(d.name) })
    }
  }
}

@Composable
private fun EmptyDevices(sharing: Boolean) {
  Surface(shape = MaterialTheme.shapes.medium, color = MaterialTheme.colorScheme.surfaceVariant, modifier = Modifier.fillMaxWidth()) {
    Row(modifier = Modifier.padding(14.dp), verticalAlignment = Alignment.CenterVertically) {
      Icon(AppIcons.Laptop, contentDescription = null, modifier = Modifier.size(22.dp), tint = MaterialTheme.colorScheme.onSurfaceVariant)
      Spacer(Modifier.width(12.dp))
      Caption(
        if (sharing) "Henüz cihaz yok. Bilgisayarda AleShare.exe'yi aç ve Ekle → Bilgisayar ile ekranındaki QR kodu tara; telefon ve tabletler Ekle → Telefon veya tablet ile eklenir."
        else "Henüz cihaz yok. Cihaz eklemek için önce paylaşımı başlat.",
        Modifier.weight(1f),
      )
    }
  }
}

@Composable
private fun DeviceRow(d: DeviceView, sharing: Boolean, enabled: Boolean, onClick: () -> Unit) {
  val state = deviceState(d, sharing)
  val rules = deviceRules(d)
  Row(
    modifier = Modifier
      .fillMaxWidth()
      .heightIn(min = 64.dp)
      .clickable(enabled = enabled, role = Role.Button, onClickLabel = "${d.name} ayrıntıları", onClick = onClick)
      .padding(vertical = 10.dp),
    verticalAlignment = Alignment.CenterVertically,
  ) {
    DeviceTile(d, state)
    Spacer(Modifier.width(14.dp))
    Column(modifier = Modifier.weight(1f)) {
      Text(d.name, style = MaterialTheme.typography.titleSmall, color = MaterialTheme.colorScheme.onSurface, maxLines = 1, overflow = TextOverflow.Ellipsis)
      Spacer(Modifier.height(2.dp))
      Text(
        state.text,
        style = MaterialTheme.typography.bodySmall,
        color = if (state.kind == DeviceState.Kind.Idle) MaterialTheme.colorScheme.onSurfaceVariant else stateColor(state.kind),
        maxLines = 1,
        overflow = TextOverflow.Ellipsis,
      )
      if (rules.isNotEmpty()) {
        Text(rules, style = MaterialTheme.typography.bodySmall, color = MaterialTheme.colorScheme.onSurfaceVariant, maxLines = 1, overflow = TextOverflow.Ellipsis)
      }
      if (d.limitMb > 0) {
        Spacer(Modifier.height(6.dp))
        LimitBar(d.usedToday, d.limitMb, Modifier.fillMaxWidth(0.85f))
      }
    }
    Spacer(Modifier.width(10.dp))
    Column(horizontalAlignment = Alignment.End) {
      Text(humanBytes(d.usedToday), style = MaterialTheme.typography.labelLarge, color = MaterialTheme.colorScheme.onSurface)
      Caption("bugün")
    }
    Spacer(Modifier.width(4.dp))
    Icon(AppIcons.ChevronRight, contentDescription = null, modifier = Modifier.size(18.dp), tint = MaterialTheme.colorScheme.onSurfaceVariant)
  }
}

@Composable
private fun LimitBar(used: Long, limitMb: Long, modifier: Modifier = Modifier) {
  val share = (used.toFloat() / (limitMb * 1024 * 1024)).coerceIn(0f, 1f)
  LinearProgressIndicator(
    progress = { share },
    modifier = modifier.height(4.dp),
    color = if (share >= 0.9f) HotspotTheme.status.warn else MaterialTheme.colorScheme.primary,
    trackColor = MaterialTheme.colorScheme.surfaceVariant,
  )
}

/** The daily limits a device can have, in MiB; 0 is none. */
private val DEVICE_LIMITS_MB = listOf(0L, 512L, 1024L, 2048L, 5120L, 10240L)

/** The speed limits to pick from, kilobits per second; 0 is none. */
private val SPEEDS_KBPS = listOf(0L, 1_000L, 2_000L, 5_000L, 10_000L, 20_000L)

/** One device's page: how it is doing, its internet on or off, its daily and speed limits, quiet hours, removing it. */
@OptIn(ExperimentalMaterial3Api::class)
@Composable
fun DeviceSheet(
  device: DeviceView,
  sharing: Boolean,
  onPause: (Boolean) -> Unit,
  onLimit: (Long) -> Unit,
  onSpeed: (Long) -> Unit,
  onQuiet: (QuietHours?) -> Unit,
  onRemove: () -> Unit,
  onDismiss: () -> Unit,
) {
  var picking by remember { mutableStateOf<Boolean?>(null) } // true: the start of the quiet hours, false: their end
  val state = deviceState(device, sharing)
  ModalBottomSheet(
    onDismissRequest = onDismiss,
    sheetState = rememberModalBottomSheetState(skipPartiallyExpanded = true),
    containerColor = MaterialTheme.colorScheme.surface,
  ) {
    Column(
      modifier = Modifier.verticalScroll(rememberScrollState()).padding(horizontal = 20.dp).padding(bottom = 28.dp),
    ) {
      Row(verticalAlignment = Alignment.CenterVertically) {
        DeviceTile(device, state, size = 52.dp)
        Spacer(Modifier.width(14.dp))
        Column(modifier = Modifier.weight(1f)) {
          Text(device.name, style = MaterialTheme.typography.titleLarge, color = MaterialTheme.colorScheme.onSurface, maxLines = 1, overflow = TextOverflow.Ellipsis)
          Text(
            (if (device.computer) "Bilgisayar" else "Telefon ya da tablet") + " · " + state.text,
            style = MaterialTheme.typography.bodyMedium,
            color = if (state.kind == DeviceState.Kind.Idle) MaterialTheme.colorScheme.onSurfaceVariant else stateColor(state.kind),
          )
        }
      }
      device.guest?.let {
        Spacer(Modifier.height(14.dp))
        Banner(Tone.Info, AppIcons.Clock, "Misafir: $it")
      }
      Spacer(Modifier.height(16.dp))
      Surface(shape = MaterialTheme.shapes.medium, color = MaterialTheme.colorScheme.surfaceVariant, modifier = Modifier.fillMaxWidth()) {
        Column(modifier = Modifier.padding(16.dp)) {
          Row {
            Column(modifier = Modifier.weight(1f)) {
              Caption("Bugün kullandığı")
              Text(humanBytes(device.usedToday), style = MaterialTheme.typography.headlineSmall, color = MaterialTheme.colorScheme.onSurface)
            }
            Column(horizontalAlignment = Alignment.End) {
              Caption("Günlük sınır")
              Text(
                if (device.limitMb > 0) limitLabel(device.limitMb) else "Yok",
                style = MaterialTheme.typography.titleMedium,
                color = MaterialTheme.colorScheme.onSurface,
              )
            }
          }
          if (device.limitMb > 0) {
            Spacer(Modifier.height(10.dp))
            LimitBar(device.usedToday, device.limitMb, Modifier.fillMaxWidth())
          }
        }
      }

      Section("İnternet erişimi") {
        Row(verticalAlignment = Alignment.CenterVertically) {
          Column(modifier = Modifier.weight(1f)) {
            Text(
              if (device.pause == Controls.Pause.None) "Açık" else "Kapalı",
              style = MaterialTheme.typography.bodyLarge,
              color = MaterialTheme.colorScheme.onSurface,
            )
            Caption(
              when (device.pause) {
                Controls.Pause.None -> "Kapatınca cihaz bağlı kalır ama internete giremez; açınca interneti hemen gelir."
                Controls.Pause.Manual -> "Sen duraklattın. Açınca interneti hemen gelir."
                Controls.Pause.Limit -> "Günlük sınır doldu. Açarsan bugün için sınır kalkar."
                Controls.Pause.Quiet -> "Kapalı saatlerin içinde. Açarsan bu seferlik açılır."
              }
            )
          }
          Spacer(Modifier.width(12.dp))
          Switch(checked = device.pause == Controls.Pause.None, onCheckedChange = { on -> onPause(!on) })
        }
      }

      Section("Günlük sınır", "Sınır dolunca bu cihazın interneti gece yarısına kadar durur ve sana bildirim gelir.") {
        Choices(
          options = (DEVICE_LIMITS_MB + listOfNotNull(device.limitMb.takeIf { it !in DEVICE_LIMITS_MB })).map { it to if (it == 0L) "Yok" else limitLabel(it) },
          selected = device.limitMb,
          onSelect = onLimit,
        )
      }

      Section("Hız sınırı", "Bu cihaz en fazla bu hızı kullanır; kalan hız öbür cihazlara kalır.") {
        Choices(
          options = (SPEEDS_KBPS + listOfNotNull(device.speedKbps.takeIf { it !in SPEEDS_KBPS })).map { it to if (it == 0L) "Sınırsız" else speedLabel(it) },
          selected = device.speedKbps,
          onSelect = onSpeed,
        )
      }

      Section("Kapalı saatler") {
        Row(verticalAlignment = Alignment.CenterVertically) {
          Caption("Bu cihaz her gün bu saatler arasında internete giremez (örneğin gece).", Modifier.weight(1f))
          Spacer(Modifier.width(12.dp))
          Switch(checked = device.quiet != null, onCheckedChange = { onQuiet(if (it) QuietHours(23 * 60, 7 * 60) else null) })
        }
        device.quiet?.let { q ->
          Spacer(Modifier.height(10.dp))
          Row(verticalAlignment = Alignment.CenterVertically, horizontalArrangement = Arrangement.spacedBy(10.dp)) {
            ClockBox("Başlangıç", QuietHours.clock(q.from), Modifier.weight(1f)) { picking = true }
            Text("→", style = MaterialTheme.typography.titleMedium, color = MaterialTheme.colorScheme.onSurfaceVariant)
            ClockBox("Bitiş", QuietHours.clock(q.to), Modifier.weight(1f)) { picking = false }
          }
        }
      }

      Spacer(Modifier.height(24.dp))
      OutlinedButton(
        onClick = onRemove,
        modifier = Modifier.fillMaxWidth().height(48.dp),
        shape = MaterialTheme.shapes.medium,
        border = BorderStroke(1.dp, MaterialTheme.colorScheme.error.copy(alpha = 0.6f)),
        colors = ButtonDefaults.outlinedButtonColors(contentColor = MaterialTheme.colorScheme.error),
      ) {
        Icon(AppIcons.Trash, contentDescription = null, modifier = Modifier.size(18.dp))
        Spacer(Modifier.width(8.dp))
        Text("Cihazı kaldır", style = MaterialTheme.typography.labelLarge)
      }
    }
  }
  val q = device.quiet
  val start = picking
  if (q != null && start != null) {
    ClockDialog(
      title = if (start) "Başlangıç" else "Bitiş",
      minutes = if (start) q.from else q.to,
      onPick = { m ->
        picking = null
        onQuiet(if (start) q.copy(from = m) else q.copy(to = m))
      },
      onDismiss = { picking = null },
    )
  }
}

/** A titled part of a device's page, set off from the one before by a line. */
@Composable
private fun Section(title: String, caption: String? = null, content: @Composable () -> Unit) {
  Spacer(Modifier.height(18.dp))
  HorizontalDivider(color = MaterialTheme.colorScheme.outlineVariant)
  Spacer(Modifier.height(16.dp))
  Text(title, style = MaterialTheme.typography.titleSmall, color = MaterialTheme.colorScheme.onSurface)
  caption?.let {
    Spacer(Modifier.height(2.dp))
    Caption(it)
  }
  Spacer(Modifier.height(10.dp))
  content()
}

/** Choices in rows that wrap, the chosen one filled. */
@OptIn(ExperimentalLayoutApi::class)
@Composable
private fun Choices(options: List<Pair<Long, String>>, selected: Long, onSelect: (Long) -> Unit) {
  val scheme = MaterialTheme.colorScheme
  FlowRow(horizontalArrangement = Arrangement.spacedBy(8.dp), verticalArrangement = Arrangement.spacedBy(8.dp)) {
    options.forEach { (value, label) ->
      val chosen = value == selected
      Surface(
        onClick = { onSelect(value) },
        shape = MaterialTheme.shapes.small,
        color = if (chosen) scheme.primary else Color.Transparent,
        border = if (chosen) null else BorderStroke(1.dp, scheme.outlineVariant),
        modifier = Modifier.height(38.dp),
      ) {
        Box(modifier = Modifier.padding(horizontal = 14.dp), contentAlignment = Alignment.Center) {
          Text(label, style = MaterialTheme.typography.labelLarge, color = if (chosen) scheme.onPrimary else scheme.onSurface)
        }
      }
    }
  }
}

/** One end of the quiet hours: a caption and the time, tapped to change it. */
@Composable
private fun ClockBox(label: String, time: String, modifier: Modifier = Modifier, onClick: () -> Unit) {
  Surface(
    onClick = onClick,
    shape = MaterialTheme.shapes.medium,
    color = MaterialTheme.colorScheme.surfaceVariant,
    modifier = modifier,
  ) {
    Column(modifier = Modifier.padding(horizontal = 14.dp, vertical = 10.dp)) {
      Caption(label)
      Text(time, style = MaterialTheme.typography.titleMedium, color = MaterialTheme.colorScheme.primary)
    }
  }
}

/** A speed limit as people say it: "5 Mbit/s", or more exactly when it is not whole megabits. */
internal fun speedLabel(kbps: Long): String = if (kbps % 1000 == 0L) "${kbps / 1000} Mbit/s" else bitRate(kbps * 1000)
