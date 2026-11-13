package dev.hotspot.app.ui

import androidx.compose.animation.animateColorAsState
import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.Spacer
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.height
import androidx.compose.foundation.layout.offset
import androidx.compose.foundation.layout.size
import androidx.compose.foundation.layout.width
import androidx.compose.material3.Icon
import androidx.compose.material3.LinearProgressIndicator
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.Text
import androidx.compose.material3.TextButton
import androidx.compose.runtime.Composable
import androidx.compose.runtime.getValue
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.unit.dp
import dev.hotspot.app.AppUpdate
import dev.hotspot.app.ClientPhase
import dev.hotspot.app.ClientUiState
import dev.hotspot.app.humanBytes
import dev.hotspot.app.ui.theme.HotspotTheme
import dev.hotspot.wgcore.Join

/**
 * The page of a device that gets its internet from another phone: nothing saved yet (scan or paste), or a
 * saved phone with a button, a status and the traffic.
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
  Reveal(state.phase == ClientPhase.Connected) {
    Panel {
      Row(modifier = Modifier.fillMaxWidth()) {
        Metric(AppIcons.ArrowUp, "Yükleme", humanBytes(state.tx), Modifier.weight(1f))
        Metric(AppIcons.ArrowDown, "İndirme", humanBytes(state.rx), Modifier.weight(1f))
      }
    }
  }
  Reveal(true) {
    Panel {
      PanelTitle("Telefon")
      Spacer(Modifier.height(8.dp))
      ValueRow("Wi-Fi ağı", saved.ssid, mono = true)
      Spacer(Modifier.height(6.dp))
      ValueRow("Adres", saved.endpoint, mono = true)
      Spacer(Modifier.height(10.dp))
      Caption("Telefonun ağı yakındayken kendiliğinden bağlanır, uzaklaşınca bağlantıyı bırakır. Sen durdurana kadar çalışır; cihaz yeniden başlatılınca da devam eder.")
      Spacer(Modifier.height(10.dp))
      // A device that was off while the phone's network got a new name or password cannot learn it by itself:
      // the phone's Wi-Fi code carries the new one, and only the Wi-Fi part of the saved connection changes.
      Caption("Telefonun ağ adı ya da parolası değiştiyse paylaşan telefonda Wi-Fi ağı kartındaki \"QR ile bağlan\" kodunu buradan tara.")
      TextButton(onClick = onScan, enabled = !locked, modifier = Modifier.offset(x = (-12).dp)) {
        Icon(AppIcons.Qr, contentDescription = null, modifier = Modifier.size(18.dp))
        Spacer(Modifier.width(8.dp))
        Text("Wi-Fi kodunu tara", style = MaterialTheme.typography.labelLarge)
      }
      Spacer(Modifier.height(4.dp))
      // Android does not bring a VPN service back after it had to end the app's process; its own always-on
      // setting does, so it is offered here instead of being left for the user to find.
      Caption("Android uygulamayı kapatırsa bağlantı kendiliğinden geri gelmez. Gelsin istersen VPN ayarlarında AleShare için \"Her zaman açık VPN\"i aç. \"VPN olmadan bağlantıları engelle\" kapalı kalsın; yoksa telefon yakında değilken internetin kesilir.")
      TextButton(onClick = onVpnSettings, enabled = !locked, modifier = Modifier.offset(x = (-12).dp)) {
        Text("VPN ayarlarını aç", style = MaterialTheme.typography.labelLarge)
      }
      TextButton(onClick = onForget, enabled = !active && !locked) {
        Icon(AppIcons.Trash, contentDescription = null, modifier = Modifier.size(18.dp))
        Spacer(Modifier.width(8.dp))
        Text("Bu telefonu sil", style = MaterialTheme.typography.labelLarge)
      }
    }
  }
}

/** The phone runs a newer release: take it over the phone's own network, then let Android install it. */
@Composable
private fun UpdatePanel(update: AppUpdate, onDownload: () -> Unit, onInstall: (String) -> Unit) {
  Panel {
    Row(verticalAlignment = Alignment.CenterVertically, horizontalArrangement = Arrangement.spacedBy(10.dp)) {
      Icon(AppIcons.Download, contentDescription = null, modifier = Modifier.size(20.dp), tint = MaterialTheme.colorScheme.primary)
      PanelTitle("AleShare ${update.name} hazır")
    }
    Spacer(Modifier.height(6.dp))
    Caption("Paylaşan telefonda daha yeni bir sürüm var. Telefonun Wi-Fi ağından indirilir, mobil veri harcamaz. Android kurmadan önce sana sorar; ayarların silinmez.")
    update.progress?.let { pct ->
      Spacer(Modifier.height(14.dp))
      LinearProgressIndicator(progress = { pct / 100f }, modifier = Modifier.fillMaxWidth())
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

@Composable
private fun NothingSaved(onScan: () -> Unit, onPaste: () -> Unit, locked: Boolean) {
  Panel {
    if (locked) {
      Banner(Tone.Info, AppIcons.Alert, LOCKED_WHILE_SHARING)
      Spacer(Modifier.height(16.dp))
    }
    Text("Başka bir telefonun internetini kullan", style = MaterialTheme.typography.headlineSmall, color = MaterialTheme.colorScheme.onSurface)
    Spacer(Modifier.height(6.dp))
    Text(
      "Paylaşan telefonda AleShare'i aç ve Cihazlar → Ekle → Telefon veya tablet'e dokun. Çıkan AleShare QR kodunu buradan tara; gerisi kendiliğinden olur.",
      style = MaterialTheme.typography.bodyMedium,
      color = MaterialTheme.colorScheme.onSurfaceVariant,
    )
    Spacer(Modifier.height(20.dp))
    ActionButton("QR kodu tara", onClick = onScan, enabled = !locked)
    Spacer(Modifier.height(4.dp))
    TextButton(onClick = onPaste, enabled = !locked, modifier = Modifier.fillMaxWidth()) { Text("Bağlantıyı yapıştır", style = MaterialTheme.typography.labelLarge) }
  }
}

@Composable
private fun ConnectPanel(saved: Join, state: ClientUiState, sharing: Boolean, active: Boolean, onConnect: () -> Unit, onStop: () -> Unit) {
  val status = HotspotTheme.status
  val idle = MaterialTheme.colorScheme.onSurfaceVariant
  val accent by animateColorAsState(
    when (state.phase) {
      ClientPhase.Connected -> status.ok
      ClientPhase.Searching, ClientPhase.Connecting, ClientPhase.Unreachable -> status.warn
      ClientPhase.Idle -> idle
    },
    label = "client-status",
  )
  Panel {
    Row(verticalAlignment = Alignment.CenterVertically, horizontalArrangement = Arrangement.spacedBy(8.dp)) {
      StatusDot(accent, pulsing = state.phase == ClientPhase.Searching || state.phase == ClientPhase.Connecting)
      Text(
        when (state.phase) {
          ClientPhase.Idle -> "Kapalı"
          ClientPhase.Searching -> "Aranıyor"
          ClientPhase.Connecting -> "Bağlanıyor"
          ClientPhase.Connected -> "Bağlı"
          ClientPhase.Unreachable -> "Yanıt yok"
        },
        style = MaterialTheme.typography.labelLarge,
        color = accent,
      )
    }
    Spacer(Modifier.height(12.dp))
    Text(
      when (state.phase) {
        ClientPhase.Idle -> "Bağlı değil"
        ClientPhase.Searching -> "Telefon aranıyor"
        ClientPhase.Connecting -> "Telefona bağlanıyor"
        ClientPhase.Connected -> "Telefona bağlı"
        ClientPhase.Unreachable -> "Telefona ulaşılamıyor"
      },
      style = MaterialTheme.typography.headlineSmall,
      color = MaterialTheme.colorScheme.onSurface,
    )
    Spacer(Modifier.height(6.dp))
    Text(
      when (state.phase) {
        ClientPhase.Idle -> "Bağlan'a dokununca bu cihaz ${saved.ssid} ağına bağlanır ve interneti telefondan alır."
        ClientPhase.Searching -> "${saved.ssid} ağı yakına gelince kendiliğinden bağlanır. Android bağlanmak için onay isterse çıkan pencerede Bağlan'a dokun. Ağın adı değiştiyse aşağıdan Wi-Fi kodunu tara."
        ClientPhase.Connecting -> "${saved.ssid} ağına bağlanıldı, telefonun yanıtı bekleniyor."
        ClientPhase.Connected -> "Bu cihaz interneti paylaşan telefondan alıyor."
        ClientPhase.Unreachable -> "Paylaşan telefonda paylaşımın açık olduğuna bak. Şimdilik kendi internetini kullanıyorsun; birazdan yeniden denenecek."
      },
      style = MaterialTheme.typography.bodyMedium,
      color = MaterialTheme.colorScheme.onSurfaceVariant,
    )
    state.error?.let {
      Spacer(Modifier.height(14.dp))
      Banner(Tone.Error, AppIcons.Alert, it)
    }
    if (sharing && !active) {
      Spacer(Modifier.height(14.dp))
      Banner(Tone.Info, AppIcons.Alert, LOCKED_WHILE_SHARING)
    }
    Spacer(Modifier.height(20.dp))
    ActionButton(
      text = if (active) "Durdur" else "Bağlan",
      onClick = if (active) onStop else onConnect,
      tonal = active,
      enabled = active || !sharing,
    )
  }
}

/** Why the page of joining another phone is locked. */
private const val LOCKED_WHILE_SHARING = "Paylaşım açıkken bu bölüm kilitli: bir cihaz hem paylaşıp hem başka bir telefona bağlanamaz. Önce Paylaş sekmesinden paylaşımı durdur."
