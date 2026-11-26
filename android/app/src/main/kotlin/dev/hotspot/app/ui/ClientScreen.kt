package dev.hotspot.app.ui

import androidx.compose.animation.animateColorAsState
import androidx.compose.foundation.background
import androidx.compose.foundation.clickable
import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Box
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.ExperimentalLayoutApi
import androidx.compose.foundation.layout.FlowRow
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.Spacer
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.height
import androidx.compose.foundation.layout.heightIn
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.layout.size
import androidx.compose.foundation.layout.width
import androidx.compose.foundation.shape.CircleShape
import androidx.compose.foundation.shape.RoundedCornerShape
import androidx.compose.material3.Button
import androidx.compose.material3.FilledTonalButton
import androidx.compose.material3.HorizontalDivider
import androidx.compose.material3.Icon
import androidx.compose.material3.LinearProgressIndicator
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.OutlinedButton
import androidx.compose.material3.Surface
import androidx.compose.material3.Text
import androidx.compose.runtime.Composable
import androidx.compose.runtime.getValue
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.draw.alpha
import androidx.compose.ui.graphics.Color
import androidx.compose.ui.graphics.vector.ImageVector
import androidx.compose.ui.semantics.Role
import androidx.compose.ui.text.style.TextOverflow
import androidx.compose.ui.unit.dp
import dev.hotspot.app.AppUpdate
import dev.hotspot.app.ClientPhase
import dev.hotspot.app.ClientUiState
import dev.hotspot.app.humanBytes
import dev.hotspot.app.ui.theme.HotspotTheme
import dev.hotspot.wgcore.Join

/**
 * The page of a device that gets its internet from another phone: nothing saved yet (how to join, scan or paste),
 * or a saved phone with its status and button, the phone's details and what can be done about it.
 */
@Composable
fun ClientPanels(
  saved: Join?,
  state: ClientUiState,
  sharing: Boolean,
  supported: Boolean,
  onScan: () -> Unit,
  onPaste: () -> Unit,
  onConnect: () -> Unit,
  onStop: () -> Unit,
  onForget: () -> Unit,
  onVpnSettings: () -> Unit,
  onDownloadUpdate: () -> Unit,
  onInstallUpdate: (String) -> Unit,
) {
  if (!supported) {
    Reveal(true) { Banner(Tone.Warning, AppIcons.Alert, "Başka bir telefona bağlanmak için Android 10 ya da üstü gerekir.") }
    return
  }
  // A phone that shares cannot use another phone's internet at the same time: the page is locked meanwhile.
  val locked = sharing && state.phase == ClientPhase.Idle
  if (saved == null) {
    Reveal(true) { NothingSaved(onScan, onPaste, locked) }
    return
  }
  val active = state.phase != ClientPhase.Idle
  Reveal(true) { ConnectPanel(saved, state, sharing, active, onConnect, onStop) }
  Reveal(state.update != null) { state.update?.let { UpdatePanel(it, onDownloadUpdate, onInstallUpdate) } }
  Reveal(true) { PhonePanel(saved, active, locked, onScan, onVpnSettings, onForget) }
}

/** The status of a connection or of sharing in a pill: a dot and a word, in [color]. */
@Composable
fun StatusPill(text: String, color: Color, pulsing: Boolean = false) {
  Surface(shape = CircleShape, color = color.copy(alpha = 0.14f)) {
    Row(modifier = Modifier.padding(horizontal = 10.dp, vertical = 5.dp), verticalAlignment = Alignment.CenterVertically, horizontalArrangement = Arrangement.spacedBy(7.dp)) {
      StatusDot(color, pulsing = pulsing)
      Text(text, style = MaterialTheme.typography.labelLarge, color = color)
    }
  }
}

@Composable
private fun NothingSaved(onScan: () -> Unit, onPaste: () -> Unit, locked: Boolean) {
  val scheme = MaterialTheme.colorScheme
  Panel {
    if (locked) {
      Banner(Tone.Info, AppIcons.Alert, LOCKED_WHILE_SHARING)
      Spacer(Modifier.height(16.dp))
    }
    Box(modifier = Modifier.size(52.dp).background(scheme.primaryContainer, RoundedCornerShape(16.dp)), contentAlignment = Alignment.Center) {
      Icon(AppIcons.Link, contentDescription = null, modifier = Modifier.size(26.dp), tint = scheme.onPrimaryContainer)
    }
    Spacer(Modifier.height(14.dp))
    Text("Başka bir telefonun internetini kullan", style = MaterialTheme.typography.headlineSmall, color = scheme.onSurface)
    Spacer(Modifier.height(6.dp))
    Caption("Bu cihaz, AleShare ile paylaşan bir telefonun ağına katılır ve internetini şifreli tünelden alır.")
    Spacer(Modifier.height(18.dp))
    Step(1, "Paylaşan telefonda AleShare'i aç: Cihazlar → Ekle → Telefon veya tablet.")
    Step(2, "Çıkan AleShare QR kodunu bu cihazla tara.")
    Step(3, "Android VPN için onay isterse ver; gerisi kendiliğinden olur.")
    Spacer(Modifier.height(18.dp))
    Button(onClick = onScan, enabled = !locked, modifier = Modifier.fillMaxWidth().height(54.dp), shape = MaterialTheme.shapes.medium) {
      Icon(AppIcons.Qr, contentDescription = null, modifier = Modifier.size(20.dp))
      Spacer(Modifier.width(10.dp))
      Text("QR kodu tara", style = MaterialTheme.typography.labelLarge)
    }
    Spacer(Modifier.height(10.dp))
    OutlinedButton(onClick = onPaste, enabled = !locked, modifier = Modifier.fillMaxWidth().height(48.dp), shape = MaterialTheme.shapes.medium) {
      Icon(AppIcons.Copy, contentDescription = null, modifier = Modifier.size(18.dp))
      Spacer(Modifier.width(8.dp))
      Text("Bağlantıyı yapıştır", style = MaterialTheme.typography.labelLarge)
    }
  }
}

/** One step of joining: its number in a circle, and what to do. */
@Composable
private fun Step(n: Int, text: String) {
  val scheme = MaterialTheme.colorScheme
  Row(modifier = Modifier.padding(vertical = 5.dp), verticalAlignment = Alignment.Top) {
    Box(modifier = Modifier.size(26.dp).background(scheme.surfaceVariant, CircleShape), contentAlignment = Alignment.Center) {
      Text("$n", style = MaterialTheme.typography.labelLarge, color = scheme.primary)
    }
    Spacer(Modifier.width(12.dp))
    Text(text, style = MaterialTheme.typography.bodyMedium, color = scheme.onSurface, modifier = Modifier.padding(top = 3.dp))
  }
}

@OptIn(ExperimentalLayoutApi::class)
@Composable
private fun ConnectPanel(saved: Join, state: ClientUiState, sharing: Boolean, active: Boolean, onConnect: () -> Unit, onStop: () -> Unit) {
  val status = HotspotTheme.status
  val scheme = MaterialTheme.colorScheme
  val accent by animateColorAsState(
    when (state.phase) {
      ClientPhase.Connected -> status.ok
      ClientPhase.Searching, ClientPhase.Connecting, ClientPhase.Unreachable -> status.warn
      ClientPhase.Idle -> scheme.onSurfaceVariant
    },
    label = "client-status",
  )
  Panel {
    StatusPill(
      when (state.phase) {
        ClientPhase.Idle -> "Kapalı"
        ClientPhase.Searching -> "Aranıyor"
        ClientPhase.Connecting -> "Bağlanıyor"
        ClientPhase.Connected -> "Bağlı"
        ClientPhase.Unreachable -> "Yanıt yok"
      },
      accent,
      pulsing = state.phase == ClientPhase.Searching || state.phase == ClientPhase.Connecting,
    )
    Spacer(Modifier.height(14.dp))
    Text(
      when (state.phase) {
        ClientPhase.Idle -> "Bağlı değil"
        ClientPhase.Searching -> "Telefon aranıyor"
        ClientPhase.Connecting -> "Telefona bağlanıyor"
        ClientPhase.Connected -> "Telefona bağlı"
        ClientPhase.Unreachable -> "Telefona ulaşılamıyor"
      },
      style = MaterialTheme.typography.headlineSmall,
      color = scheme.onSurface,
    )
    Spacer(Modifier.height(6.dp))
    FlowRow(horizontalArrangement = Arrangement.spacedBy(6.dp), verticalArrangement = Arrangement.spacedBy(6.dp)) {
      Surface(shape = CircleShape, color = scheme.surfaceVariant) {
        Row(modifier = Modifier.padding(start = 9.dp, end = 11.dp, top = 5.dp, bottom = 5.dp), verticalAlignment = Alignment.CenterVertically, horizontalArrangement = Arrangement.spacedBy(6.dp)) {
          Icon(AppIcons.Wifi, contentDescription = null, modifier = Modifier.size(14.dp), tint = scheme.onSurfaceVariant)
          Text(saved.ssid, style = MaterialTheme.typography.labelMedium, color = scheme.onSurface, maxLines = 1, overflow = TextOverflow.Ellipsis)
        }
      }
    }
    Spacer(Modifier.height(10.dp))
    Caption(
      when (state.phase) {
        ClientPhase.Idle -> "Bağlan'a dokununca bu cihaz telefonun ağına katılır ve interneti ondan alır."
        ClientPhase.Searching -> "Ağ yakına gelince kendiliğinden bağlanır. Android onay isterse çıkan pencerede Bağlan'a dokun. Ağın adı değiştiyse aşağıdan Wi-Fi kodunu tara."
        ClientPhase.Connecting -> "Ağa katılındı, telefonun yanıtı bekleniyor."
        ClientPhase.Connected -> "Bu cihaz interneti paylaşan telefondan alıyor."
        ClientPhase.Unreachable -> "Paylaşan telefonda paylaşımın açık olduğuna bak; birazdan yeniden denenecek."
      }
    )
    if (state.phase == ClientPhase.Connected) {
      Spacer(Modifier.height(14.dp))
      Surface(shape = MaterialTheme.shapes.medium, color = scheme.surfaceVariant, modifier = Modifier.fillMaxWidth()) {
        Row(modifier = Modifier.padding(horizontal = 14.dp, vertical = 10.dp)) {
          Metric(AppIcons.ArrowDown, "İndirilen", humanBytes(state.rx), Modifier.weight(1f))
          Metric(AppIcons.ArrowUp, "Yüklenen", humanBytes(state.tx), Modifier.weight(1f))
        }
      }
    }
    if (state.phase == ClientPhase.Searching || state.phase == ClientPhase.Connecting) {
      Spacer(Modifier.height(14.dp))
      LinearProgressIndicator(modifier = Modifier.fillMaxWidth().height(3.dp), color = status.warn, trackColor = scheme.surfaceVariant)
    }
    state.error?.let {
      Spacer(Modifier.height(14.dp))
      Banner(Tone.Error, AppIcons.Alert, it)
    }
    if (sharing && !active) {
      Spacer(Modifier.height(14.dp))
      Banner(Tone.Info, AppIcons.Alert, LOCKED_WHILE_SHARING)
    }
    Spacer(Modifier.height(18.dp))
    val size = Modifier.fillMaxWidth().height(54.dp)
    if (active) {
      FilledTonalButton(onClick = onStop, modifier = size, shape = MaterialTheme.shapes.medium) {
        Text("Durdur", style = MaterialTheme.typography.labelLarge)
      }
    } else {
      Button(onClick = onConnect, enabled = !sharing, modifier = size, shape = MaterialTheme.shapes.medium) {
        Icon(AppIcons.Link, contentDescription = null, modifier = Modifier.size(20.dp))
        Spacer(Modifier.width(10.dp))
        Text("Bağlan", style = MaterialTheme.typography.labelLarge)
      }
    }
  }
}

/** The phone this device uses: its network and address, and what can be done about it. */
@Composable
private fun PhonePanel(saved: Join, active: Boolean, locked: Boolean, onScan: () -> Unit, onVpnSettings: () -> Unit, onForget: () -> Unit) {
  val scheme = MaterialTheme.colorScheme
  Panel {
    PanelTitle("Paylaşan telefon")
    Spacer(Modifier.height(10.dp))
    Surface(shape = MaterialTheme.shapes.medium, color = scheme.surfaceVariant, modifier = Modifier.fillMaxWidth()) {
      Column(modifier = Modifier.padding(horizontal = 14.dp, vertical = 10.dp)) {
        ValueRow("Wi-Fi ağı", saved.ssid, mono = true)
        HorizontalDivider(modifier = Modifier.padding(vertical = 8.dp), color = scheme.outlineVariant)
        ValueRow("Adres", saved.endpoint, mono = true)
      }
    }
    Spacer(Modifier.height(10.dp))
    Caption("Telefonun ağı yakındayken kendiliğinden bağlanır, uzaklaşınca bırakır. Sen durdurana kadar çalışır; cihaz yeniden başlatılınca da devam eder.")
    Spacer(Modifier.height(8.dp))
    // A device that was off while the phone's network got a new name or password cannot learn it by itself:
    // the phone's Wi-Fi code carries the new one, and only the Wi-Fi part of the saved connection changes.
    ActionRow(
      AppIcons.Qr, "Wi-Fi kodunu tara",
      "Telefonun ağ adı ya da parolası değiştiyse, paylaşan telefonda Wi-Fi ağı kartındaki QR kodunu tara.",
      enabled = !locked, onClick = onScan,
    )
    // Android does not bring a VPN service back after it had to end the app's process; its own always-on
    // setting does, so it is offered here instead of being left for the user to find.
    ActionRow(
      AppIcons.Shield, "Her zaman açık VPN",
      "Android uygulamayı kapatırsa bağlantı kendiliğinden geri gelsin diye VPN ayarlarında AleShare için aç. \"VPN olmadan bağlantıları engelle\" kapalı kalsın.",
      enabled = !locked, onClick = onVpnSettings,
    )
    ActionRow(
      AppIcons.Trash, "Bu telefonu sil",
      if (active) "Silmek için önce bağlantıyı durdur." else "Bu cihaz o telefona bir daha bağlanmaz; yeniden kullanmak için QR kodu baştan taranır.",
      enabled = !active && !locked, danger = true, onClick = onForget,
    )
  }
}

/** Something to do about the saved phone: an icon, what it does in a line or two, and an arrow. */
@Composable
private fun ActionRow(icon: ImageVector, title: String, caption: String, enabled: Boolean, danger: Boolean = false, onClick: () -> Unit) {
  val scheme = MaterialTheme.colorScheme
  val tint = if (danger) scheme.error else scheme.onPrimaryContainer
  Row(
    modifier = Modifier.fillMaxWidth().heightIn(min = 56.dp).alpha(if (enabled) 1f else 0.38f)
      .clickable(enabled = enabled, role = Role.Button, onClick = onClick).padding(vertical = 8.dp),
    verticalAlignment = Alignment.CenterVertically,
  ) {
    Box(
      modifier = Modifier.size(36.dp).background(if (danger) scheme.error.copy(alpha = 0.14f) else scheme.primaryContainer, RoundedCornerShape(10.dp)),
      contentAlignment = Alignment.Center,
    ) {
      Icon(icon, contentDescription = null, modifier = Modifier.size(19.dp), tint = tint)
    }
    Spacer(Modifier.width(14.dp))
    Column(modifier = Modifier.weight(1f)) {
      Text(title, style = MaterialTheme.typography.bodyLarge, color = if (danger) scheme.error else scheme.onSurface)
      Caption(caption)
    }
    Spacer(Modifier.width(8.dp))
    Icon(AppIcons.ChevronRight, contentDescription = null, modifier = Modifier.size(18.dp), tint = scheme.onSurfaceVariant)
  }
}

/** The phone runs a newer release: take it over the phone's own network, then let Android install it. */
@Composable
private fun UpdatePanel(update: AppUpdate, onDownload: () -> Unit, onInstall: (String) -> Unit) {
  val scheme = MaterialTheme.colorScheme
  Panel {
    Row(verticalAlignment = Alignment.CenterVertically) {
      Box(modifier = Modifier.size(36.dp).background(scheme.primaryContainer, RoundedCornerShape(10.dp)), contentAlignment = Alignment.Center) {
        Icon(AppIcons.Download, contentDescription = null, modifier = Modifier.size(19.dp), tint = scheme.onPrimaryContainer)
      }
      Spacer(Modifier.width(12.dp))
      PanelTitle("AleShare ${update.name} hazır")
    }
    Spacer(Modifier.height(8.dp))
    Caption("Paylaşan telefonda daha yeni bir sürüm var. Telefonun Wi-Fi ağından indirilir, mobil veri harcamaz. Android kurmadan önce sana sorar; ayarların silinmez.")
    update.progress?.let { pct ->
      Spacer(Modifier.height(14.dp))
      LinearProgressIndicator(progress = { pct / 100f }, modifier = Modifier.fillMaxWidth(), trackColor = scheme.surfaceVariant)
      Spacer(Modifier.height(6.dp))
      Caption("%$pct indirildi")
    }
    update.error?.let {
      Spacer(Modifier.height(14.dp))
      Banner(Tone.Error, AppIcons.Alert, it)
    }
    Spacer(Modifier.height(16.dp))
    val file = update.file
    ActionButton(
      text = when {
        update.progress != null -> "İndiriliyor"
        file != null -> "Kur"
        else -> "İndir ve kur"
      },
      onClick = { if (file != null) onInstall(file) else onDownload() },
      tonal = file == null,
      enabled = update.progress == null,
    )
  }
}

/** Why the page of joining another phone is locked. */
private const val LOCKED_WHILE_SHARING = "Paylaşım açıkken bu bölüm kilitli: bir cihaz hem paylaşıp hem başka bir telefona bağlanamaz. Önce Paylaş sekmesinden paylaşımı durdur."
