package dev.hotspot.app.ui

import androidx.compose.foundation.Image
import androidx.compose.foundation.background
import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Box
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.Spacer
import androidx.compose.foundation.layout.aspectRatio
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.height
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.layout.size
import androidx.compose.foundation.layout.width
import androidx.compose.foundation.shape.CircleShape
import androidx.compose.foundation.text.KeyboardActions
import androidx.compose.foundation.text.KeyboardOptions
import androidx.compose.material3.AlertDialog
import androidx.compose.material3.Button
import androidx.compose.material3.ButtonDefaults
import androidx.compose.material3.ExperimentalMaterial3Api
import androidx.compose.material3.Icon
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.ModalBottomSheet
import androidx.compose.material3.OutlinedTextField
import androidx.compose.material3.Surface
import androidx.compose.material3.Text
import androidx.compose.material3.TextButton
import androidx.compose.material3.rememberModalBottomSheetState
import androidx.compose.runtime.Composable
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.remember
import androidx.compose.runtime.setValue
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.draw.alpha
import androidx.compose.ui.draw.clip
import androidx.compose.ui.graphics.Color
import androidx.compose.ui.graphics.ImageBitmap
import androidx.compose.ui.graphics.vector.ImageVector
import androidx.compose.ui.text.font.FontFamily
import androidx.compose.ui.text.font.FontWeight
import androidx.compose.ui.text.input.ImeAction
import androidx.compose.ui.text.input.KeyboardType
import androidx.compose.ui.text.input.PasswordVisualTransformation
import androidx.compose.ui.text.input.VisualTransformation
import androidx.compose.ui.text.style.TextAlign
import androidx.compose.ui.unit.dp
import androidx.compose.ui.unit.sp
import androidx.compose.foundation.rememberScrollState
import androidx.compose.foundation.verticalScroll
import androidx.compose.foundation.selection.selectable
import androidx.compose.material3.LinearProgressIndicator
import androidx.compose.material3.RadioButton
import androidx.compose.ui.semantics.Role
import androidx.compose.foundation.layout.ExperimentalLayoutApi
import androidx.compose.foundation.layout.FlowRow
import androidx.compose.material3.FilterChip
import androidx.compose.material3.Switch
import androidx.compose.material3.TimePicker
import androidx.compose.material3.rememberTimePickerState
import dev.hotspot.app.OnlineUpdate
import dev.hotspot.app.QuietHours
import dev.hotspot.app.PairingRequest
import dev.hotspot.app.WifiCredentials
import dev.hotspot.app.WifiNetwork
import dev.hotspot.app.humanBytes
import dev.hotspot.app.limitLabel

/**
 * What to add. A Windows PC shows a QR code in its setup and this phone's camera reads it; that needs sharing
 * on, since the computer joins this phone's network first. A phone or tablet gets a QR code from here.
 */
@OptIn(ExperimentalMaterial3Api::class)
@Composable
fun AddDeviceSheet(canScanQr: Boolean, sharing: Boolean, onComputer: () -> Unit, onPhone: () -> Unit, onGuest: () -> Unit, onDismiss: () -> Unit) {
  ModalBottomSheet(
    onDismissRequest = onDismiss,
    sheetState = rememberModalBottomSheetState(skipPartiallyExpanded = true),
    containerColor = MaterialTheme.colorScheme.surface,
  ) {
    Column(modifier = Modifier.padding(horizontal = 20.dp).padding(bottom = 24.dp), verticalArrangement = Arrangement.spacedBy(10.dp)) {
      Text("Cihaz ekle", style = MaterialTheme.typography.titleLarge)
      Caption("Eklediğin cihaz bu telefonun mobil internetini kullanır.")
      Spacer(Modifier.height(4.dp))
      ChoiceRow(
        icon = AppIcons.Laptop,
        title = "Bilgisayar (Windows)",
        subtitle = if (sharing) "Bilgisayarda AleShare.exe'yi aç ve ekranında çıkan QR kodu tara. Program: github.com/${OnlineUpdate.REPO}"
        else "Önce paylaşımı başlat; bilgisayar bu telefonun ağına bağlanarak eşleşir.",
        enabled = sharing,
        onClick = onComputer,
      )
      ChoiceRow(
        icon = AppIcons.Phone,
        title = "Telefon veya tablet",
        subtitle = if (canScanQr) "Öbür cihaz QR kodu AleShare ya da WireGuard ile tarar." else "Önce paylaşımı başlat.",
        enabled = canScanQr,
        onClick = onPhone,
      )
      ChoiceRow(
        icon = AppIcons.Clock,
        title = "Misafir",
        subtitle = if (canScanQr) "Bir arkadaşın için süreli ya da sınırlı erişim. Süre ya da veri bitince kendiliğinden silinir."
        else "Önce paylaşımı başlat.",
        enabled = canScanQr,
        onClick = onGuest,
      )
    }
  }
}

/** A row with an icon and two lines; tappable only when [onClick] is given. */
@Composable
private fun ChoiceRow(icon: ImageVector, title: String, subtitle: String, enabled: Boolean = true, onClick: (() -> Unit)? = null) {
  val content: @Composable () -> Unit = {
    Row(modifier = Modifier.padding(14.dp), verticalAlignment = Alignment.CenterVertically) {
      Box(
        modifier = Modifier.size(44.dp).background(MaterialTheme.colorScheme.primaryContainer, CircleShape),
        contentAlignment = Alignment.Center,
      ) {
        Icon(icon, contentDescription = null, modifier = Modifier.size(22.dp), tint = MaterialTheme.colorScheme.onPrimaryContainer)
      }
      Spacer(Modifier.width(14.dp))
      Column(modifier = Modifier.weight(1f)) {
        Text(title, style = MaterialTheme.typography.titleSmall, color = MaterialTheme.colorScheme.onSurface)
        Caption(subtitle)
      }
    }
  }
  val modifier = Modifier.fillMaxWidth().alpha(if (enabled) 1f else 0.5f)
  if (onClick == null) {
    Surface(modifier = modifier, shape = MaterialTheme.shapes.medium, color = MaterialTheme.colorScheme.surfaceVariant, content = content)
  } else {
    Surface(onClick = onClick, enabled = enabled, modifier = modifier, shape = MaterialTheme.shapes.medium, color = MaterialTheme.colorScheme.surfaceVariant, content = content)
  }
}

/** The QR code a phone's camera reads to join the Wi-Fi network without typing its password. */
@Composable
fun WifiQrDialog(ssid: String, qr: ImageBitmap, onDismiss: () -> Unit) {
  AlertDialog(
    onDismissRequest = onDismiss,
    shape = MaterialTheme.shapes.large,
    title = { Text(ssid) },
    text = {
      Column(horizontalAlignment = Alignment.CenterHorizontally, verticalArrangement = Arrangement.spacedBy(12.dp)) {
        Box(modifier = Modifier.fillMaxWidth().clip(MaterialTheme.shapes.medium).background(Color.White).padding(8.dp)) {
          Image(bitmap = qr, contentDescription = "Wi-Fi QR kodu", modifier = Modifier.fillMaxWidth().aspectRatio(1f))
        }
        Caption("Öbür cihazın kamerasıyla okut; parola yazmadan ağa bağlanır. Kodu, ağa girmesini istemediğin kimseye gösterme.")
      }
    },
    confirmButton = { TextButton(onClick = onDismiss) { Text("Kapat") } },
  )
}

/**
 * A new name and password for the Wi-Fi network. Android wants the name to begin with "DIRECT-" and two
 * characters, so only what follows [WifiNetwork.PREFIX] is the user's. [connected] is how many paired devices
 * use the network now: they learn the change by themselves.
 */
@Composable
fun ChangeWifiDialog(current: WifiCredentials, connected: Int, onChange: (WifiCredentials) -> Unit, onDismiss: () -> Unit) {
  // A name from before 1.0.31 had "HS-" after "DIRECT-": offered without it, so saving drops it.
  var name by remember { mutableStateOf(current.ssid.removePrefix(WifiNetwork.OLD_PREFIX).removePrefix(WifiNetwork.PREFIX)) }
  var password by remember { mutableStateOf(current.passphrase) }
  var showPassword by remember { mutableStateOf(false) }
  val nameProblem = WifiNetwork.nameProblem(name)
  val passwordProblem = WifiNetwork.passwordProblem(password)
  val next = WifiCredentials(WifiNetwork.PREFIX + name, password)
  val ok = nameProblem == null && passwordProblem == null && next != current
  AlertDialog(
    onDismissRequest = onDismiss,
    shape = MaterialTheme.shapes.large,
    title = { Text("Ağ adı ve parola") },
    text = {
      Column(modifier = Modifier.verticalScroll(rememberScrollState()), verticalArrangement = Arrangement.spacedBy(12.dp)) {
        OutlinedTextField(
          value = name,
          onValueChange = { name = it.trim() },
          label = { Text("Ağ adı") },
          prefix = { Text(WifiNetwork.PREFIX, fontFamily = FontFamily.Monospace) },
          textStyle = MaterialTheme.typography.bodyLarge.copy(fontFamily = FontFamily.Monospace),
          singleLine = true,
          isError = nameProblem != null,
          supportingText = { Text(nameProblem ?: "Android, Wi-Fi Direct ağlarının adının başına DIRECT- koyar.") },
          shape = MaterialTheme.shapes.medium,
          keyboardOptions = KeyboardOptions(autoCorrectEnabled = false, imeAction = ImeAction.Next),
        )
        OutlinedTextField(
          value = password,
          onValueChange = { password = it },
          label = { Text("Parola") },
          singleLine = true,
          isError = passwordProblem != null,
          supportingText = { Text(passwordProblem ?: "${password.length} karakter") },
          textStyle = MaterialTheme.typography.bodyLarge.copy(fontFamily = FontFamily.Monospace),
          visualTransformation = if (showPassword) VisualTransformation.None else PasswordVisualTransformation(),
          trailingIcon = {
            IconAction(if (showPassword) AppIcons.EyeOff else AppIcons.Eye, if (showPassword) "Parolayı gizle" else "Parolayı göster", onClick = { showPassword = !showPassword })
          },
          shape = MaterialTheme.shapes.medium,
          keyboardOptions = KeyboardOptions(autoCorrectEnabled = false, keyboardType = KeyboardType.Password, imeAction = ImeAction.Done),
          keyboardActions = KeyboardActions(onDone = { if (ok) onChange(next) }),
        )
        TextButton(onClick = { password = WifiNetwork.randomPassword(); showPassword = true }) { Text("Rastgele parola") }
        Caption(
          if (connected > 0) "Şu an bağlı $connected bilgisayar ya da tablet yeni adı ve parolayı kendiliğinden öğrenir; telefon onları bekler (en çok 1,5 dakika), sonra ağı değiştirir."
          else "Şu an bağlı bilgisayar ya da tablet yok; ağ hemen değişir."
        )
        Caption("Kapalı ya da uzakta olan cihazları yeni ağa bir kez senin bağlaman gerekir: bilgisayarda Windows'un Wi-Fi listesinden, tablette Bağlan → Wi-Fi kodunu tara ile. iPhone ve öbür cihazlar yeni ağa elle bağlanır.")
      }
    },
    confirmButton = { Button(enabled = ok, onClick = { onChange(next) }, shape = MaterialTheme.shapes.medium) { Text("Değiştir") } },
    dismissButton = { TextButton(onClick = onDismiss) { Text("Vazgeç") } },
  )
}

/** Asks for the name of the phone or tablet to add. */
@Composable
fun NameDeviceDialog(onCreate: (String) -> Unit, onDismiss: () -> Unit) {
  var name by remember { mutableStateOf("") }
  AlertDialog(
    onDismissRequest = onDismiss,
    shape = MaterialTheme.shapes.large,
    title = { Text("Telefon veya tablet") },
    text = {
      Column(verticalArrangement = Arrangement.spacedBy(12.dp)) {
        Caption("Listede görünecek bir ad ver. Sonra bu cihaz için bir QR kod çıkar.")
        // The keyboard can cover the dialog's buttons (a phone held sideways): the keyboard's own Done does it too.
        OutlinedTextField(
          value = name,
          onValueChange = { name = it },
          label = { Text("Cihaz adı") },
          singleLine = true,
          shape = MaterialTheme.shapes.medium,
          keyboardOptions = KeyboardOptions(imeAction = ImeAction.Done),
          keyboardActions = KeyboardActions(onDone = { if (name.isNotBlank()) onCreate(name.trim()) }),
        )
      }
    },
    confirmButton = { TextButton(enabled = name.isNotBlank(), onClick = { onCreate(name.trim()) }) { Text("QR oluştur") } },
    dismissButton = { TextButton(onClick = onDismiss) { Text("Vazgeç") } },
  )
}

/** Which app on the new device reads the code: Hotspot, its camera (the Wi-Fi network), or WireGuard. */
enum class QrKind { Hotspot, Wifi, WireGuard }

/**
 * The QR code of a freshly made device, with its private key inside; it is shown once. For another phone running
 * this app one scan joins the Wi-Fi network and opens the tunnel ([hotspot] is null when the app did not make
 * the network, which the code could not describe); the official WireGuard app takes the plain configuration.
 */
@Composable
fun QrDialog(
  name: String,
  hotspot: ImageBitmap?,
  wireGuard: ImageBitmap,
  onSave: () -> Unit,
  onShare: () -> Unit,
  onCopyLink: () -> Unit,
  onDismiss: () -> Unit,
  wifi: ImageBitmap? = null,
  guestNote: String? = null,
) {
  // A guest most likely has no Hotspot app: the camera joins the Wi-Fi network, then WireGuard takes the tunnel.
  var kind by remember { mutableStateOf(if (guestNote != null && wifi != null) QrKind.Wifi else if (hotspot != null) QrKind.Hotspot else QrKind.WireGuard) }
  AlertDialog(
    onDismissRequest = onDismiss,
    shape = MaterialTheme.shapes.large,
    title = { Text(name) },
    text = {
      Column(horizontalAlignment = Alignment.CenterHorizontally, verticalArrangement = Arrangement.spacedBy(12.dp)) {
        guestNote?.let { Caption("$it. Süre ya da veri bitince erişimi kendiliğinden silinir.") }
        val kinds = listOfNotNull(
          (QrKind.Hotspot to "AleShare").takeIf { hotspot != null },
          (QrKind.Wifi to "1. Wi-Fi").takeIf { wifi != null && guestNote != null },
          (QrKind.WireGuard to if (wifi != null && guestNote != null) "2. WireGuard" else "WireGuard"),
        )
        if (kinds.size > 1) SegmentedSwitch(options = kinds, selected = kind, onSelect = { kind = it })
        // Scanners need dark modules on a light ground, whatever the app's theme.
        Box(modifier = Modifier.fillMaxWidth().clip(MaterialTheme.shapes.medium).background(Color.White).padding(8.dp)) {
          Image(
            bitmap = when {
              kind == QrKind.Hotspot && hotspot != null -> hotspot
              kind == QrKind.Wifi && wifi != null -> wifi
              else -> wireGuard
            },
            contentDescription = "QR kodu",
            modifier = Modifier.fillMaxWidth().aspectRatio(1f),
          )
        }
        when (kind) {
          QrKind.Hotspot -> {
            Caption("Öbür cihazda AleShare'i aç ve Bağlan sekmesinden bu QR kodu tara; gerisi kendiliğinden olur. Bu kod yalnızca o cihaz içindir ve kapatınca bir daha gösterilmez.")
            TextButton(onClick = onCopyLink) { Text("Bağlantıyı kopyala") }
          }
          QrKind.Wifi -> Caption("Önce bu kodu misafirin telefonunun kamerasıyla okut; telefon Wi-Fi ağına bağlanır. Sonra \"2. WireGuard\" koduna geç.")
          QrKind.WireGuard -> {
            Caption(
              (if (guestNote != null) "Misafirin telefonuna WireGuard uygulamasını kur (App Store ya da Google Play). Uygulamada + → QR kodundan tara'ya dokunup kodu okut ve bağlantıyı aç."
              else "WireGuard uygulamasında + → QR kodundan tara'ya dokunup kodu okut, sonra cihazı Wi-Fi ağına elle bağla.") +
                " Bu kod yalnızca o cihaz içindir ve kapatınca bir daha gösterilmez."
            )
            Row(horizontalArrangement = Arrangement.spacedBy(4.dp)) {
              TextButton(onClick = onSave) { Text("Dosyaya kaydet") }
              TextButton(onClick = onShare) { Text("Paylaş") }
            }
          }
        }
      }
    },
    confirmButton = { TextButton(onClick = onDismiss) { Text("Kapat") } },
  )
}

/** A join link that was scanned or pasted: what it would do is shown before anything is saved. */
@Composable
fun JoinConfirmDialog(join: dev.hotspot.wgcore.Join, onConfirm: () -> Unit, onDismiss: () -> Unit) {
  AlertDialog(
    onDismissRequest = onDismiss,
    shape = MaterialTheme.shapes.large,
    title = { Text("Bu telefona bağlanılsın mı?") },
    text = {
      Column(verticalArrangement = Arrangement.spacedBy(12.dp)) {
        ValueRow("Wi-Fi ağı", join.ssid, mono = true)
        ValueRow("Telefonun adresi", join.endpoint, mono = true)
        Caption("Bu cihazın bütün interneti bu telefondan geçer; telefonun sahibi hangi adreslere bağlandığını görebilir. Kodu tanımadığın birinden aldıysan Vazgeç'e dokun.")
      }
    },
    confirmButton = { Button(onClick = onConfirm, shape = MaterialTheme.shapes.medium) { Text("Kaydet ve bağlan") } },
    dismissButton = { TextButton(onClick = onDismiss) { Text("Vazgeç") } },
  )
}

/** Where a link is pasted when scanning does not work (no Play services, a link sent in a message). */
@Composable
fun PasteLinkDialog(initial: String, onUse: (String) -> Unit, onDismiss: () -> Unit) {
  var text by remember { mutableStateOf(initial) }
  AlertDialog(
    onDismissRequest = onDismiss,
    shape = MaterialTheme.shapes.large,
    title = { Text("Bağlantıyı yapıştır") },
    text = {
      Column(verticalArrangement = Arrangement.spacedBy(12.dp)) {
        Caption("Paylaşan telefonda QR kodun altındaki \"Bağlantıyı kopyala\" ile kopyaladığın metni buraya yapıştır.")
        OutlinedTextField(
          value = text,
          onValueChange = { text = it },
          label = { Text("hotspot://join?d=…") },
          maxLines = 4,
          shape = MaterialTheme.shapes.medium,
        )
      }
    },
    confirmButton = { TextButton(enabled = text.isNotBlank(), onClick = { onUse(text) }) { Text("Devam") } },
    dismissButton = { TextButton(onClick = onDismiss) { Text("Vazgeç") } },
  )
}

/** A computer asks to join: the user compares the number with the one on its screen. */
@Composable
fun PairingRequestDialog(request: PairingRequest) {
  AlertDialog(
    onDismissRequest = { request.respond(false) },
    shape = MaterialTheme.shapes.large,
    title = { Text("Eşleşme isteği") },
    text = {
      Column(modifier = Modifier.fillMaxWidth(), horizontalAlignment = Alignment.CenterHorizontally, verticalArrangement = Arrangement.spacedBy(12.dp)) {
        Text(request.name, style = MaterialTheme.typography.titleMedium)
        Surface(shape = MaterialTheme.shapes.medium, color = MaterialTheme.colorScheme.surfaceVariant, modifier = Modifier.fillMaxWidth()) {
          Text(
            request.sas,
            modifier = Modifier.padding(vertical = 18.dp),
            textAlign = TextAlign.Center,
            style = MaterialTheme.typography.headlineMedium,
            fontFamily = FontFamily.Monospace,
            fontWeight = FontWeight.SemiBold,
            letterSpacing = 6.sp,
            color = MaterialTheme.colorScheme.onSurface,
          )
        }
        Text(
          "Bilgisayarın ekranındaki numara bununla aynıysa Eşleştir'e dokun. Farklıysa ya da bu isteği sen başlatmadıysan Reddet'e dokun.",
          style = MaterialTheme.typography.bodyMedium,
          color = MaterialTheme.colorScheme.onSurfaceVariant,
        )
      }
    },
    confirmButton = { Button(onClick = { request.respond(true) }, shape = MaterialTheme.shapes.medium) { Text("Eşleştir") } },
    dismissButton = { TextButton(onClick = { request.respond(false) }) { Text("Reddet") } },
  )
}

/**
 * How to take the Quick Settings tile called [name] out of the panel. Android lets no app remove its own tile
 * (switching its service off leaves a dead tile on Samsung phones), so the user does it, in three short steps.
 */
@Composable
fun RemoveTileDialog(name: String, onDismiss: () -> Unit) {
  AlertDialog(
    onDismissRequest = onDismiss,
    shape = MaterialTheme.shapes.large,
    title = { Text("Panelden kaldır") },
    text = {
      Column(verticalArrangement = Arrangement.spacedBy(14.dp)) {
        Text(
          "Android, düğmeyi panelden yalnızca senin kaldırmana izin verir.",
          style = MaterialTheme.typography.bodyMedium,
          color = MaterialTheme.colorScheme.onSurfaceVariant,
        )
        Step(1, "Bildirim panelini tamamen aşağı çek.")
        Step(2, "Kalem (düzenle) simgesine dokun.")
        Step(3, "$name düğmesini basılı tutup panelin dışına sürükle.")
      }
    },
    confirmButton = { TextButton(onClick = onDismiss) { Text("Tamam") } },
  )
}

/** One numbered step of a short instruction. */
@Composable
private fun Step(number: Int, text: String) {
  Row(verticalAlignment = Alignment.CenterVertically) {
    Box(
      modifier = Modifier.size(26.dp).background(MaterialTheme.colorScheme.primaryContainer, CircleShape),
      contentAlignment = Alignment.Center,
    ) {
      Text(number.toString(), style = MaterialTheme.typography.labelLarge, color = MaterialTheme.colorScheme.onPrimaryContainer)
    }
    Spacer(Modifier.width(12.dp))
    Text(text, style = MaterialTheme.typography.bodyMedium, color = MaterialTheme.colorScheme.onSurface)
  }
}

@Composable
fun RemoveDeviceDialog(name: String, onRemove: () -> Unit, onDismiss: () -> Unit) {
  AlertDialog(
    onDismissRequest = onDismiss,
    shape = MaterialTheme.shapes.large,
    title = { Text("Cihaz kaldırılsın mı?") },
    text = { Text("$name artık bağlanamaz. Yeniden kullanmak için baştan eklemen gerekir.", style = MaterialTheme.typography.bodyMedium) },
    confirmButton = {
      TextButton(onClick = onRemove, colors = ButtonDefaults.textButtonColors(contentColor = MaterialTheme.colorScheme.error)) { Text("Kaldır") }
    },
    dismissButton = { TextButton(onClick = onDismiss) { Text("Vazgeç") } },
  )
}

/**
 * The computer's code was read: it is added the moment it asks, which it does every two seconds. Stays until
 * then, or until the user closes it (the code stays good for ten minutes either way).
 */
@Composable
fun ComputerWaitDialog(onDismiss: () -> Unit) {
  AlertDialog(
    onDismissRequest = onDismiss,
    shape = MaterialTheme.shapes.large,
    icon = { Icon(AppIcons.Laptop, contentDescription = null) },
    title = { Text("Bilgisayar ekleniyor") },
    text = {
      Column(verticalArrangement = Arrangement.spacedBy(12.dp)) {
        LinearProgressIndicator(modifier = Modifier.fillMaxWidth())
        Text(
          "Kod okundu. Bilgisayar birkaç saniye içinde kendiliğinden eklenir; uygulamayı ekranda açık tut.",
          style = MaterialTheme.typography.bodyMedium,
          color = MaterialTheme.colorScheme.onSurfaceVariant,
        )
      }
    },
    confirmButton = { TextButton(onClick = onDismiss) { Text("Kapat") } },
  )
}

/** The daily limits a device can have, in MiB; 0 is none. */
private val DEVICE_LIMITS_MB = listOf(0L, 512L, 1024L, 2048L, 5120L, 10240L)

/** One device: what it used today, pausing it, its daily limit, speed limit and quiet hours, and removing it. */
@OptIn(ExperimentalLayoutApi::class)
@Composable
fun DeviceDialog(
  name: String,
  usedToday: Long,
  limitMb: Long,
  paused: Boolean,
  onPause: (Boolean) -> Unit,
  onLimit: (Long) -> Unit,
  speedKbps: Long,
  onSpeed: (Long) -> Unit,
  quiet: QuietHours?,
  onQuiet: (QuietHours?) -> Unit,
  guestNote: String?,
  onRemove: () -> Unit,
  onDismiss: () -> Unit,
) {
  var picking by remember { mutableStateOf<Boolean?>(null) } // true: the start of the quiet hours, false: their end
  AlertDialog(
    onDismissRequest = onDismiss,
    shape = MaterialTheme.shapes.large,
    title = { Text(name) },
    text = {
      Column(modifier = Modifier.verticalScroll(rememberScrollState())) {
        guestNote?.let {
          Text(it, style = MaterialTheme.typography.bodyMedium, color = MaterialTheme.colorScheme.primary)
          Spacer(Modifier.height(4.dp))
        }
        Text(
          "Bugün ${humanBytes(usedToday)} kullandı." + if (paused) " İnterneti şu an duraklatılmış." else "",
          style = MaterialTheme.typography.bodyMedium,
          color = MaterialTheme.colorScheme.onSurfaceVariant,
        )
        Spacer(Modifier.height(14.dp))
        Button(onClick = { onPause(!paused) }, modifier = Modifier.fillMaxWidth().height(48.dp), shape = MaterialTheme.shapes.medium) {
          Text(if (paused) "Devam ettir" else "İnterneti duraklat")
        }
        Spacer(Modifier.height(6.dp))
        Text(
          "Duraklatılan cihaz bağlı kalır ama internete giremez. Devam ettirince interneti hemen gelir.",
          style = MaterialTheme.typography.bodySmall,
          color = MaterialTheme.colorScheme.onSurfaceVariant,
        )
        Spacer(Modifier.height(16.dp))
        Text("Günlük sınır", style = MaterialTheme.typography.titleSmall, color = MaterialTheme.colorScheme.onSurface)
        Text(
          "Sınır dolunca bu cihazın interneti gece yarısına kadar durur ve sana bildirim gelir.",
          style = MaterialTheme.typography.bodySmall,
          color = MaterialTheme.colorScheme.onSurfaceVariant,
        )
        Spacer(Modifier.height(4.dp))
        (DEVICE_LIMITS_MB + listOfNotNull(limitMb.takeIf { it !in DEVICE_LIMITS_MB })).forEach { mb ->
          Row(
            modifier = Modifier.fillMaxWidth().selectable(selected = mb == limitMb, onClick = { onLimit(mb) }, role = Role.RadioButton).padding(vertical = 6.dp),
            verticalAlignment = Alignment.CenterVertically,
          ) {
            RadioButton(selected = mb == limitMb, onClick = null)
            Spacer(Modifier.width(12.dp))
            Text(if (mb == 0L) "Sınır yok" else limitLabel(mb), style = MaterialTheme.typography.bodyLarge, color = MaterialTheme.colorScheme.onSurface)
          }
        }
        Spacer(Modifier.height(16.dp))
        Text("Hız sınırı", style = MaterialTheme.typography.titleSmall, color = MaterialTheme.colorScheme.onSurface)
        Text(
          "Bu cihaz en fazla bu hızı kullanır; kalan hız öbür cihazlara kalır.",
          style = MaterialTheme.typography.bodySmall,
          color = MaterialTheme.colorScheme.onSurfaceVariant,
        )
        Spacer(Modifier.height(6.dp))
        FlowRow(horizontalArrangement = Arrangement.spacedBy(8.dp)) {
          (SPEEDS_KBPS + listOfNotNull(speedKbps.takeIf { it !in SPEEDS_KBPS })).forEach { kbps ->
            FilterChip(
              selected = kbps == speedKbps,
              onClick = { onSpeed(kbps) },
              label = { Text(if (kbps == 0L) "Sınırsız" else "${kbps / 1000} Mbit/s") },
            )
          }
        }
        Spacer(Modifier.height(16.dp))
        Row(verticalAlignment = Alignment.CenterVertically) {
          Column(modifier = Modifier.weight(1f)) {
            Text("İnternetin kapalı olduğu saatler", style = MaterialTheme.typography.titleSmall, color = MaterialTheme.colorScheme.onSurface)
            Text(
              "Bu cihaz her gün bu saatler arasında internete giremez (örneğin gece).",
              style = MaterialTheme.typography.bodySmall,
              color = MaterialTheme.colorScheme.onSurfaceVariant,
            )
          }
          Spacer(Modifier.width(12.dp))
          Switch(checked = quiet != null, onCheckedChange = { onQuiet(if (it) QuietHours(23 * 60, 7 * 60) else null) })
        }
        quiet?.let { q ->
          Row(verticalAlignment = Alignment.CenterVertically) {
            TextButton(onClick = { picking = true }) { Text(QuietHours.clock(q.from), style = MaterialTheme.typography.titleMedium) }
            Text("–", color = MaterialTheme.colorScheme.onSurfaceVariant)
            TextButton(onClick = { picking = false }) { Text(QuietHours.clock(q.to), style = MaterialTheme.typography.titleMedium) }
          }
        }
      }
    },
    confirmButton = { TextButton(onClick = onDismiss) { Text("Kapat") } },
    dismissButton = {
      TextButton(onClick = onRemove, colors = ButtonDefaults.textButtonColors(contentColor = MaterialTheme.colorScheme.error)) { Text("Cihazı kaldır") }
    },
  )
  val q = quiet
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

/** The speed limits to pick from, kilobits per second; 0 is none. */
private val SPEEDS_KBPS = listOf(0L, 1_000L, 2_000L, 5_000L, 10_000L, 20_000L)

/** Picks a time of day, on a 24-hour clock. */
@OptIn(ExperimentalMaterial3Api::class)
@Composable
private fun ClockDialog(title: String, minutes: Int, onPick: (Int) -> Unit, onDismiss: () -> Unit) {
  val state = rememberTimePickerState(initialHour = minutes / 60, initialMinute = minutes % 60, is24Hour = true)
  AlertDialog(
    onDismissRequest = onDismiss,
    shape = MaterialTheme.shapes.large,
    title = { Text(title) },
    text = { TimePicker(state = state) },
    confirmButton = { TextButton(onClick = { onPick(state.hour * 60 + state.minute) }) { Text("Tamam") } },
    dismissButton = { TextButton(onClick = onDismiss) { Text("Vazgeç") } },
  )
}

/** The choices for stopping sharing when nobody is connected, in minutes; 0 is never. */
private val IDLE_MINUTES = listOf(0L, 15L, 30L, 60L, 120L)

@Composable
fun IdleStopDialog(current: Long, onPick: (Long) -> Unit, onDismiss: () -> Unit) {
  AlertDialog(
    onDismissRequest = onDismiss,
    shape = MaterialTheme.shapes.large,
    title = { Text("Kimse bağlı değilken kapat") },
    text = {
      Column {
        Text(
          "Bu süre boyunca hiçbir cihaz bağlı olmazsa paylaşım kendiliğinden kapanır ve bildirim gelir. Böylece pil boşa harcanmaz.",
          style = MaterialTheme.typography.bodyMedium,
          color = MaterialTheme.colorScheme.onSurfaceVariant,
        )
        Spacer(Modifier.height(10.dp))
        (IDLE_MINUTES + listOfNotNull(current.takeIf { it !in IDLE_MINUTES })).forEach { m ->
          Row(
            modifier = Modifier.fillMaxWidth().selectable(selected = m == current, onClick = { onPick(m) }, role = Role.RadioButton).padding(vertical = 8.dp),
            verticalAlignment = Alignment.CenterVertically,
          ) {
            RadioButton(selected = m == current, onClick = null)
            Spacer(Modifier.width(12.dp))
            Text(if (m == 0L) "Kapalı" else idleLabel(m), style = MaterialTheme.typography.bodyLarge, color = MaterialTheme.colorScheme.onSurface)
          }
        }
      }
    },
    confirmButton = { TextButton(onClick = onDismiss) { Text("Kapat") } },
  )
}

/** The monthly plan: its size in GB and the day of the month it renews; an empty size removes the plan. */
@Composable
fun PlanDialog(currentMb: Long, currentDay: Int, onSave: (Long, Int) -> Unit, onDismiss: () -> Unit) {
  var gb by remember { mutableStateOf(if (currentMb > 0) (currentMb / 1024.0).let { if (it % 1 == 0.0) it.toLong().toString() else it.toString() } else "") }
  var day by remember { mutableStateOf(currentDay.toString()) }
  val size = gb.replace(',', '.').toDoubleOrNull()
  val renew = day.toIntOrNull()
  val valid = (gb.isBlank() || (size != null && size > 0 && size <= 10_000)) && renew != null && renew in 1..31
  AlertDialog(
    onDismissRequest = onDismiss,
    shape = MaterialTheme.shapes.large,
    title = { Text("Aylık paket") },
    text = {
      Column(verticalArrangement = Arrangement.spacedBy(12.dp)) {
        Caption("Operatörünün verdiği aylık mobil veri paketi. Paketin %90'ı kullanılınca ve paket bitince bildirim gelir; paylaşım durmaz.")
        OutlinedTextField(
          value = gb,
          onValueChange = { gb = it.filter { c -> c.isDigit() || c == ',' || c == '.' }.take(7) },
          label = { Text("Paket (GB)") },
          placeholder = { Text("Örneğin 30") },
          singleLine = true,
          shape = MaterialTheme.shapes.medium,
          keyboardOptions = KeyboardOptions(keyboardType = KeyboardType.Decimal, imeAction = ImeAction.Next),
        )
        OutlinedTextField(
          value = day,
          onValueChange = { day = it.filter { c -> c.isDigit() }.take(2) },
          label = { Text("Ayın kaçında yenileniyor? (1–31)") },
          singleLine = true,
          shape = MaterialTheme.shapes.medium,
          keyboardOptions = KeyboardOptions(keyboardType = KeyboardType.Number, imeAction = ImeAction.Done),
        )
      }
    },
    confirmButton = {
      TextButton(enabled = valid, onClick = { onSave(size?.let { (it * 1024).toLong() } ?: 0L, renew ?: 1) }) { Text("Kaydet") }
    },
    dismissButton = { TextButton(onClick = onDismiss) { Text("Vazgeç") } },
  )
}

/** The guest's name, and for how long and how much data. */
@OptIn(ExperimentalLayoutApi::class)
@Composable
fun GuestDialog(taken: List<String>, onCreate: (name: String, hours: Long, mb: Long) -> Unit, onDismiss: () -> Unit) {
  val suggested = remember(taken) { generateSequence(1) { it + 1 }.map { if (it == 1) "Misafir" else "Misafir $it" }.first { it !in taken } }
  var name by remember { mutableStateOf(suggested) }
  var hours by remember { mutableStateOf(3L) }
  var mb by remember { mutableStateOf(1024L) }
  val clean = name.trim()
  AlertDialog(
    onDismissRequest = onDismiss,
    shape = MaterialTheme.shapes.large,
    title = { Text("Misafir") },
    text = {
      Column(modifier = Modifier.verticalScroll(rememberScrollState()), verticalArrangement = Arrangement.spacedBy(10.dp)) {
        Caption("Süre ya da veri bitince misafirin erişimi kendiliğinden silinir. Misafirin telefonunda WireGuard uygulaması olmalı.")
        OutlinedTextField(
          value = name,
          onValueChange = { name = it.take(30) },
          label = { Text("Ad") },
          singleLine = true,
          shape = MaterialTheme.shapes.medium,
          isError = clean in taken,
          supportingText = if (clean in taken) ({ Text("Bu adda bir cihaz zaten var.") }) else null,
        )
        Text("Süre", style = MaterialTheme.typography.titleSmall, color = MaterialTheme.colorScheme.onSurface)
        FlowRow(horizontalArrangement = Arrangement.spacedBy(8.dp)) {
          listOf(1L to "1 saat", 3L to "3 saat", 24L to "1 gün", 0L to "Süresiz").forEach { (h, label) ->
            FilterChip(selected = h == hours, onClick = { hours = h }, label = { Text(label) })
          }
        }
        Text("Veri", style = MaterialTheme.typography.titleSmall, color = MaterialTheme.colorScheme.onSurface)
        FlowRow(horizontalArrangement = Arrangement.spacedBy(8.dp)) {
          listOf(500L, 1024L, 2048L, 0L).forEach { m ->
            FilterChip(selected = m == mb, onClick = { mb = m }, label = { Text(if (m == 0L) "Sınırsız" else limitLabel(m)) })
          }
        }
      }
    },
    confirmButton = {
      TextButton(enabled = clean.isNotEmpty() && clean !in taken && (hours > 0 || mb > 0), onClick = { onCreate(clean, hours, mb) }) { Text("QR oluştur") }
    },
    dismissButton = { TextButton(onClick = onDismiss) { Text("Vazgeç") } },
  )
}

/** The daily limits to pick from, in MiB; 0 is none. */
private val LIMITS_MB = listOf(0L, 1024L, 2048L, 5120L, 10240L, 20480L, 51200L)

/** Picks the daily mobile data limit; sharing stops when it is reached. */
@Composable
fun LimitDialog(currentMb: Long, onPick: (Long) -> Unit, onDismiss: () -> Unit) {
  AlertDialog(
    onDismissRequest = onDismiss,
    shape = MaterialTheme.shapes.large,
    title = { Text("Günlük veri sınırı") },
    text = {
      Column {
        Text(
          "Bugün paylaşılan veri bu sınıra ulaşınca paylaşım durur ve bildirim gelir. Sınırın %90'ına gelince önceden haber verilir. Sayaç gece yarısı sıfırlanır.",
          style = MaterialTheme.typography.bodyMedium,
          color = MaterialTheme.colorScheme.onSurfaceVariant,
        )
        Spacer(Modifier.height(10.dp))
        (LIMITS_MB + listOfNotNull(currentMb.takeIf { it !in LIMITS_MB })).forEach { mb ->
          Row(
            modifier = Modifier.fillMaxWidth().selectable(selected = mb == currentMb, onClick = { onPick(mb) }, role = Role.RadioButton).padding(vertical = 8.dp),
            verticalAlignment = Alignment.CenterVertically,
          ) {
            RadioButton(selected = mb == currentMb, onClick = null)
            Spacer(Modifier.width(12.dp))
            Text(if (mb == 0L) "Sınır yok" else limitLabel(mb), style = MaterialTheme.typography.bodyLarge, color = MaterialTheme.colorScheme.onSurface)
          }
        }
      }
    },
    confirmButton = { TextButton(onClick = onDismiss) { Text("Kapat") } },
  )
}

@Composable
fun ProblemDialog(message: String, onDismiss: () -> Unit) {
  AlertDialog(
    onDismissRequest = onDismiss,
    shape = MaterialTheme.shapes.large,
    title = { Text("Bir sorun çıktı") },
    text = { Text(message, style = MaterialTheme.typography.bodyMedium) },
    confirmButton = { TextButton(onClick = onDismiss) { Text("Tamam") } },
  )
}
