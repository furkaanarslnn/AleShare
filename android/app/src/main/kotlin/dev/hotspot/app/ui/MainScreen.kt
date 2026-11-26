package dev.hotspot.app.ui

import android.Manifest
import android.app.Activity
import android.app.StatusBarManager
import android.content.ComponentName
import android.graphics.drawable.Icon
import androidx.annotation.RequiresApi
import dev.hotspot.app.R
import androidx.compose.ui.graphics.vector.ImageVector
import dev.hotspot.app.ShareTileService
import dev.hotspot.app.SimCard
import dev.hotspot.app.Source
import dev.hotspot.app.setSourcePreference
import dev.hotspot.app.sourcePreference
import dev.hotspot.app.sourceProblem
import android.content.ClipData
import android.content.ClipDescription
import android.content.ClipboardManager
import android.content.Context
import android.content.Intent
import android.content.pm.PackageManager
import android.net.Uri
import android.net.VpnService
import android.net.wifi.WifiManager
import android.os.Build
import android.os.PersistableBundle
import android.os.PowerManager
import android.os.SystemClock
import android.provider.Settings
import android.os.Handler
import android.os.Looper
import android.widget.Toast
import androidx.activity.compose.rememberLauncherForActivityResult
import androidx.activity.result.contract.ActivityResultContracts
import androidx.compose.animation.AnimatedVisibility
import androidx.compose.animation.animateColorAsState
import androidx.compose.animation.core.animateFloatAsState
import androidx.compose.animation.expandVertically
import androidx.compose.animation.fadeIn
import androidx.compose.animation.fadeOut
import androidx.compose.animation.shrinkVertically
import androidx.compose.foundation.clickable
import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.PaddingValues
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.Spacer
import androidx.compose.foundation.layout.WindowInsets
import androidx.compose.foundation.layout.Box
import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.height
import androidx.compose.foundation.layout.heightIn
import androidx.compose.foundation.layout.offset
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.layout.safeDrawing
import androidx.compose.foundation.layout.size
import androidx.compose.foundation.layout.width
import androidx.compose.foundation.layout.widthIn
import androidx.compose.foundation.layout.windowInsetsPadding
import androidx.compose.foundation.rememberScrollState
import androidx.compose.foundation.selection.selectable
import androidx.compose.foundation.shape.RoundedCornerShape
import androidx.compose.foundation.selection.toggleable
import androidx.compose.foundation.verticalScroll
import androidx.compose.material3.FilledTonalButton
import androidx.compose.material3.OutlinedButton
import androidx.compose.foundation.background
import androidx.compose.foundation.shape.CircleShape
import androidx.compose.material3.HorizontalDivider
import androidx.compose.material3.Icon
import androidx.compose.material3.LinearProgressIndicator
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.RadioButton
import androidx.compose.material3.Surface
import androidx.compose.material3.Switch
import androidx.compose.material3.Text
import androidx.compose.material3.TextButton
import androidx.compose.runtime.Composable
import androidx.compose.runtime.LaunchedEffect
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.remember
import androidx.compose.runtime.rememberUpdatedState
import androidx.compose.runtime.rememberCoroutineScope
import androidx.compose.runtime.saveable.rememberSaveable
import androidx.compose.runtime.setValue
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.draw.alpha
import androidx.compose.ui.draw.rotate
import androidx.compose.ui.graphics.ImageBitmap
import androidx.compose.ui.graphics.asImageBitmap
import androidx.compose.ui.platform.LocalContext
import androidx.compose.ui.res.painterResource
import androidx.compose.ui.semantics.Role
import androidx.compose.ui.text.font.FontFamily
import androidx.compose.ui.text.style.TextOverflow
import androidx.compose.ui.unit.dp
import androidx.core.content.ContextCompat
import androidx.core.content.FileProvider
import java.io.File
import androidx.lifecycle.Lifecycle
import androidx.lifecycle.compose.LocalLifecycleOwner
import androidx.lifecycle.compose.collectAsStateWithLifecycle
import androidx.lifecycle.repeatOnLifecycle
import com.google.mlkit.vision.barcode.common.Barcode
import com.google.mlkit.vision.codescanner.GmsBarcodeScannerOptions
import com.google.mlkit.vision.codescanner.GmsBarcodeScanning
import dev.hotspot.app.ClientPhase
import dev.hotspot.app.ClientService
import dev.hotspot.app.ClientState
import dev.hotspot.app.ClientStore
import dev.hotspot.app.Controls
import dev.hotspot.app.CoreHolder
import dev.hotspot.app.DataUsage
import dev.hotspot.app.GatewayService
import dev.hotspot.app.limitLabel
import dev.hotspot.app.GatewayState
import dev.hotspot.app.GatewayUiState
import dev.hotspot.app.JoinInbox
import dev.hotspot.app.QrPhoto
import dev.hotspot.app.LocalAddress
import dev.hotspot.app.PairingBridge
import dev.hotspot.app.TrafficStats
import dev.hotspot.app.WifiChange
import dev.hotspot.app.connectedFollowers
import dev.hotspot.app.setSplitPreference
import dev.hotspot.app.splitPreference
import dev.hotspot.app.WifiCredentials
import dev.hotspot.app.WifiNetwork
import dev.hotspot.app.humanBytes
import dev.hotspot.app.localAddresses
import dev.hotspot.app.ownNetworkPreference
import dev.hotspot.app.qrBitmap
import dev.hotspot.app.toTraffic
import dev.hotspot.app.ui.theme.HotspotTheme
import dev.hotspot.wgcore.Core
import dev.hotspot.wgcore.Join
import dev.hotspot.wgcore.Wgcore
import kotlinx.coroutines.delay
import kotlinx.coroutines.launch
import kotlinx.coroutines.Dispatchers
import kotlinx.coroutines.withContext
import androidx.compose.foundation.Canvas
import androidx.compose.foundation.ScrollState
import androidx.compose.foundation.layout.ColumnScope
import androidx.compose.foundation.layout.consumeWindowInsets
import androidx.compose.material3.Badge
import androidx.compose.material3.BadgedBox
import androidx.compose.material3.NavigationBar
import androidx.compose.material3.NavigationBarItem
import androidx.compose.material3.Scaffold
import androidx.compose.ui.geometry.CornerRadius
import androidx.compose.ui.geometry.Offset
import androidx.compose.ui.geometry.Size
import dev.hotspot.app.Guard
import dev.hotspot.app.Guests
import dev.hotspot.app.Plan
import dev.hotspot.app.Updates
import dev.hotspot.app.SpeedTest
import dev.hotspot.app.OnlineUpdate
import dev.hotspot.app.QuietHours
import dev.hotspot.app.Tr
import dev.hotspot.app.bitRate
import dev.hotspot.app.bottleneck

private const val PAIRING_REOPEN_MILLIS = 30_000L
private const val ACTIVE_PEER_SECONDS = 75L
/** How often the screen looks whether a SIM card is there (see SimCard). */
private const val SIM_LOOK_MILLIS = 3_000L

/** How long "Paylaşımı başlat" waits for the user to switch Wi-Fi on before it lets go. */
private const val WIFI_WAIT_MILLIS = 120_000L

private fun wifiOn(context: Context): Boolean = context.getSystemService(WifiManager::class.java)?.isWifiEnabled == true

/**
 * Has Wi-Fi switched on: the system's Wi-Fi panel on Android 10 and later (apps may not switch it themselves any
 * more; the user turns it on there with one tap), directly before that.
 */
private fun turnWifiOn(context: Context) {
  if (Build.VERSION.SDK_INT >= 29) {
    runCatching { context.startActivity(Intent(Settings.Panel.ACTION_WIFI)) }
      .onFailure { runCatching { context.startActivity(Intent(Settings.ACTION_WIFI_SETTINGS)) } }
  } else {
    @Suppress("DEPRECATION")
    context.getSystemService(WifiManager::class.java)?.isWifiEnabled = true
  }
}

/**
 * A freshly paired device. The config holds its private key and exists only here, in memory. [link] is the
 * same tunnel as one join link for another phone running this app; null when the app made no Wi-Fi network.
 */
private class Pairing(
  val name: String,
  val config: String,
  val qr: ImageBitmap,
  val link: String?,
  val linkQr: ImageBitmap?,
  val wifiQr: ImageBitmap? = null,
  val guestNote: String? = null,
)

/**
 * The pairing code on the screen. It lives outside the screen's own state: turning the phone builds the screen
 * anew, and a code that is shown only once must not be lost to that (the device is in the list by then, and
 * without its code it would have to be removed and added again). It goes when the user closes it.
 */
private val shownPairing = mutableStateOf<Pairing?>(null)

/** The address Android gives the owner of a Wi-Fi Direct group, on every phone. */
private const val OWN_NETWORK_ADDRESS = "192.168.49.1"

/**
 * Where a device that is added finds this phone. On the app's own network that is always the same address,
 * whether sharing runs right now or not; the address the phone has at this moment on another network (the Wi-Fi
 * at home, while sharing is off) is not it, and a device given that one never connected. Without the app's own
 * network it is the address picked in the settings.
 */
internal fun deviceEndpoint(ownNetwork: Boolean, pickedIp: String?, port: Int): String =
  if (ownNetwork || pickedIp == null) "$OWN_NETWORK_ADDRESS:$port" else "$pickedIp:$port"

/** Share the internet of this phone, or use the internet of another phone. */
/** The app's sections, in the bar at the bottom: sharing, data, settings, and joining another phone. */
private enum class Tab(val title: String, val label: String) {
  Share("AleShare", "Paylaş"),
  Connect("Bağlan", "Bağlan"),
  Data("Veri", "Veri"),
  Settings("Ayarlar", "Ayarlar"),
}

@Composable
fun MainScreen() {
  val context = LocalContext.current
  val state by GatewayState.state.collectAsStateWithLifecycle()
  val client by ClientState.state.collectAsStateWithLifecycle()
  val core = remember { CoreHolder.get(context) }
  val lifecycleOwner = LocalLifecycleOwner.current

  var tab by rememberSaveable { mutableStateOf(Tab.Share) }
  var startWhenWifiUntil by remember { mutableStateOf(0L) } // elapsed time; 0: not waiting for Wi-Fi
  var connectedCount by remember { mutableStateOf(0) }
  // The devices whose tunnel carried something a moment ago, and those that are computers (see DeviceView).
  var activeNames by remember { mutableStateOf(emptySet<String>()) }
  var computers by remember { mutableStateOf(runCatching { core.computerNames().lines().filter { it.isNotBlank() }.toSet() }.getOrDefault(emptySet())) }
  // Bytes per second down and up, one reading a second, the newest last: the live graph's last minute.
  var rateHistory by remember { mutableStateOf(emptyList<Pair<Long, Long>>()) }
  var savedJoin by remember { mutableStateOf(ClientStore.load(context)) }
  var pendingLink by remember { mutableStateOf<String?>(null) }
  var pendingJoin by remember { mutableStateOf<Join?>(null) }
  var showPaste by remember { mutableStateOf(false) }
  val incoming by JoinInbox.link.collectAsStateWithLifecycle()

  val pairingRequest by PairingBridge.request.collectAsStateWithLifecycle()
  var peers by remember { mutableStateOf(peerNames(core)) }
  var addresses by remember { mutableStateOf(localAddresses()) }
  var chosenIp by remember { mutableStateOf<String?>(null) }
  var traffic by remember { mutableStateOf<TrafficStats?>(null) }
  var ignoringBattery by remember { mutableStateOf(isIgnoringBattery(context)) }
  var usageAccess by remember { mutableStateOf(Plan.hasUsageAccess(context)) }
  var showSheet by remember { mutableStateOf(false) }
  var wifiQr by remember { mutableStateOf<ImageBitmap?>(null) }
  var showName by remember { mutableStateOf(false) }
  var pairing by shownPairing
  var removing by remember { mutableStateOf<String?>(null) }
  var problem by remember { mutableStateOf<String?>(null) }
  val ownNetwork = remember { ownNetworkPreference(context) } // always: the app opens its own network
  var showPassword by remember { mutableStateOf(false) }
  var advanced by rememberSaveable { mutableStateOf(false) }
  var changingWifi by remember { mutableStateOf(false) }
  var tileAdded by remember { mutableStateOf(ShareTileService.added(context)) }
  var removingTile by remember { mutableStateOf<String?>(null) }
  var splitTls by remember { mutableStateOf(splitPreference(context)) }
  var blockAds by remember { mutableStateOf(Controls.blockAds(context)) }
  var saveData by remember { mutableStateOf(Controls.saveData(context)) }
  var priority by remember { mutableStateOf(Controls.priority(context)) }
  var band5 by remember { mutableStateOf(Controls.band5(context)) }
  var guard by remember { mutableStateOf(Controls.guard(context)) }
  var idleMin by remember { mutableStateOf(Controls.idleStopMin(context)) }
  var choosingIdle by remember { mutableStateOf(false) }
  var plan by remember { mutableStateOf<Plan.Status?>(null) }
  var planDays by remember { mutableStateOf<List<Plan.Day>>(emptyList()) }
  var planTick by remember { mutableStateOf(0) } // read the plan again
  var editingPlan by remember { mutableStateOf(false) }
  var addingGuest by remember { mutableStateOf(false) }
  val speed by SpeedTest.state.collectAsStateWithLifecycle()
  LaunchedEffect(Unit) { OnlineUpdate.check(context) } // is a newer release out on GitHub? (every few hours)
  var deviceSheet by remember { mutableStateOf<String?>(null) }
  var deviceTick by remember { mutableStateOf(0) } // read the devices' limits and pauses again
  var usage by remember { mutableStateOf(DataUsage.today(context)) }
  var limitMb by remember { mutableStateOf(DataUsage.limitMb(context)) }
  var choosingLimit by remember { mutableStateOf(false) }
  var waitingComputer by remember { mutableStateOf(false) }
  val pairedEvent by PairingBridge.pairedEvents.collectAsStateWithLifecycle()
  // Read again when the network changes: the user may give it a new name or password. A change of the password
  // alone leaves the name as it was, so the end of a change (and a change that failed) counts too.
  val wifi = remember(state.network, state.wifiChange, state.error, state.running) { WifiNetwork.credentials(context) }

  val running by rememberUpdatedState(state.running)
  val onShareTab by rememberUpdatedState(tab == Tab.Share)

  // A computer joined (by its QR code or its number): show it in the list at once, and say so after a scan.
  LaunchedEffect(pairedEvent) {
    val event = pairedEvent ?: return@LaunchedEffect
    peers = peerNames(core)
    if (waitingComputer) {
      waitingComputer = false
      Toast.makeText(context, "${event.name} eklendi", Toast.LENGTH_SHORT).show()
    }
  }

  // The connection follows the phone when its network changes: show the name it uses now.
  LaunchedEffect(client.ssid) { savedJoin = ClientStore.load(context) }

  // A newer release has just come from the phone: go straight on to installing it (the panel keeps a "Kur" button).
  var installOffered by rememberSaveable { mutableStateOf<String?>(null) } // once, not again on every rotation
  LaunchedEffect(client.update?.file) {
    val file = client.update?.file
    if (file != null && file != installOffered) {
      installOffered = file
      installUpdate(context, file)
    }
  }

  // A link that was tapped outside the app opens the page of the one who connects.
  LaunchedEffect(incoming) {
    incoming?.let { link ->
      tab = Tab.Connect
      handleLink(link, onJoin = { pendingJoin = it; pendingLink = link }, onProblem = { problem = it })
      JoinInbox.consume()
    }
  }

  // Only poll while the screen is visible, so the phone is not woken up for nothing.
  LaunchedEffect(Unit) {
    lifecycleOwner.repeatOnLifecycle(Lifecycle.State.RESUMED) {
      var lastOpened = 0L
      try {
        while (true) {
          addresses = localAddresses()
          // Both may have been allowed in the system settings a moment ago.
          ignoringBattery = isIgnoringBattery(context)
          Plan.hasUsageAccess(context).let {
            if (it != usageAccess) {
              usageAccess = it
              planTick++ // the plan counts the whole phone from now on
            }
          }
          tileAdded = ShareTileService.added(context) // the user may take the tile out in the panel itself
          splitTls = splitPreference(context) // the notification's button may have switched the mode
          // A paired computer may have changed these from its window.
          blockAds = Controls.blockAds(context)
          saveData = Controls.saveData(context)
          priority = Controls.priority(context)
          guard = Controls.guard(context)
          deviceTick++
          usage = DataUsage.today(context) // the service adds to it while sharing
          peers = peerNames(core)
          // As the notification counts them: devices whose tunnel carried something a moment ago.
          activeNames = if (running) core.activePeers(ACTIVE_PEER_SECONDS).lines().filter { it.isNotBlank() }.toSet() else emptySet()
          connectedCount = activeNames.size
          computers = core.computerNames().lines().filter { it.isNotBlank() }.toSet()
          // A computer can ask to join while sharing is on and this screen is open, with nothing to press first;
          // it still needs the number to be confirmed here. A window ends after one pairing or three minutes
          // and is then opened again, but not more than once in half a minute, so a stream of bad requests
          // cannot keep the phone busy.
          val open = core.pairing()
          val now = SystemClock.elapsedRealtime()
          if (running && onShareTab && !open && now - lastOpened >= PAIRING_REOPEN_MILLIS) {
            runCatching { PairingBridge.start(context, core) }
            lastOpened = now
          } else if (!(running && onShareTab) && open) {
            core.stopPairing()
          }
          delay(2000)
        }
      } finally {
        // Leaving the screen closes the window: nobody can ask for a pairing that nobody is there to answer.
        core.stopPairing()
      }
    }
  }
  // The monthly plan reads the phone's data use from the system: off the main thread, twice a minute at most.
  LaunchedEffect(planTick) {
    SpeedTest.load(context)
    lifecycleOwner.repeatOnLifecycle(Lifecycle.State.RESUMED) {
      while (true) {
        withContext(Dispatchers.IO) {
          plan = runCatching { Plan.status(context) }.getOrNull()
          planDays = runCatching { Plan.days(context) }.getOrDefault(emptyList())
        }
        delay(30_000)
      }
    }
  }
  LaunchedEffect(state.running) {
    if (!state.running) {
      traffic = null
      showPassword = false
      rateHistory = emptyList()
      return@LaunchedEffect
    }
    lifecycleOwner.repeatOnLifecycle(Lifecycle.State.RESUMED) {
      var before: TrafficStats? = null
      while (true) {
        val now = core.stats().toTraffic()
        before?.let {
          val rate = (now.bytesDown - it.bytesDown).coerceAtLeast(0) to (now.bytesUp - it.bytesUp).coerceAtLeast(0)
          rateHistory = (rateHistory + rate).takeLast(RATE_HISTORY)
        }
        before = now
        traffic = now
        delay(1000)
      }
    }
  }

  val selected = addresses.firstOrNull { it.ip == chosenIp } ?: addresses.firstOrNull()

  // Whatever the user answers, start: the service says what is missing instead of failing silently.
  val permissions = rememberLauncherForActivityResult(ActivityResultContracts.RequestMultiplePermissions()) {
    GatewayService.start(context, ownNetwork = ownNetwork)
  }
  val begin: () -> Unit = {
    if (ownNetwork && !wifiOn(context)) {
      // The app's own network is Wi-Fi Direct, on the Wi-Fi radio: have it switched on, then start by itself.
      GatewayState.update { it.copy(error = null) }
      startWhenWifiUntil = SystemClock.elapsedRealtime() + WIFI_WAIT_MILLIS
      turnWifiOn(context)
    } else {
      val wanted = buildList {
        if (Build.VERSION.SDK_INT >= 33) add(Manifest.permission.POST_NOTIFICATIONS)
        if (ownNetwork) {
          if (Build.VERSION.SDK_INT >= 33) {
            add(Manifest.permission.NEARBY_WIFI_DEVICES)
          } else {
            // Android 12 shows nothing at all when the precise location is asked for without the approximate one.
            add(Manifest.permission.ACCESS_FINE_LOCATION)
            add(Manifest.permission.ACCESS_COARSE_LOCATION)
          }
        }
      }.filter { ContextCompat.checkSelfPermission(context, it) != PackageManager.PERMISSION_GRANTED }
      if (wanted.isEmpty()) GatewayService.start(context, ownNetwork = ownNetwork) else permissions.launch(wanted.toTypedArray())
    }
  }
  // Wi-Fi was off at "Paylaşımı başlat": start once the user has switched it on (in the system's panel), for a while.
  LaunchedEffect(startWhenWifiUntil) {
    if (startWhenWifiUntil == 0L) return@LaunchedEffect
    while (SystemClock.elapsedRealtime() < startWhenWifiUntil) {
      if (wifiOn(context)) {
        startWhenWifiUntil = 0L
        begin()
        return@LaunchedEffect
      }
      delay(500)
    }
    startWhenWifiUntil = 0L
  }
  // QR codes: Google's scanner where Google Play services are there, else a photo from the phone's own camera app
  // that the app reads itself (QrPhoto). Without the second way a phone without those services could not add a
  // computer at all.
  val scope = rememberCoroutineScope()
  var photoScan by remember { mutableStateOf<((String) -> Unit)?>(null) }
  val photo = remember { QrPhoto.uri(context) }
  val takePhoto = rememberLauncherForActivityResult(ActivityResultContracts.TakePicture()) { taken ->
    val onText = photoScan
    photoScan = null
    if (taken && onText != null) {
      scope.launch {
        val text = withContext(Dispatchers.Default) { QrPhoto.decode(context, photo) }
        if (text != null) onText(text)
        else problem = "Fotoğrafta QR kod bulunamadı. Kodu ortaya alıp yakından, net bir fotoğraf çekerek yeniden dene."
      }
    }
  }
  val scan: ((String) -> Unit) -> Unit = { onText ->
    scanLink(context, onLink = onText, onUnavailable = {
      photoScan = onText
      runCatching { takePhoto.launch(photo) }.onFailure {
        photoScan = null
        problem = "QR kodu okutacak bir kamera uygulaması bulunamadı. Bağlantıyı yapıştırmayı dene."
      }
    })
  }
  // Joining another phone's network: the system asks once for the right to be a VPN, and for notifications.
  val vpnConsent = rememberLauncherForActivityResult(ActivityResultContracts.StartActivityForResult()) {
    if (it.resultCode == Activity.RESULT_OK) ClientService.start(context)
  }
  val notifications = rememberLauncherForActivityResult(ActivityResultContracts.RequestPermission()) { }
  val connect: () -> Unit = {
    if (Build.VERSION.SDK_INT >= 33 && ContextCompat.checkSelfPermission(context, Manifest.permission.POST_NOTIFICATIONS) != PackageManager.PERMISSION_GRANTED) {
      notifications.launch(Manifest.permission.POST_NOTIFICATIONS)
    }
    val consent = VpnService.prepare(context)
    if (consent == null) ClientService.start(context) else vpnConsent.launch(consent)
  }
  val saveFile = rememberLauncherForActivityResult(ActivityResultContracts.CreateDocument("application/octet-stream")) { uri ->
    val p = pairing
    if (uri != null && p != null) {
      runCatching { context.contentResolver.openOutputStream(uri)?.use { it.write(p.config.toByteArray()) } }
        .onFailure { problem = it.message ?: it.toString() }
    }
  }

  // This device joined another phone (Bağlan): it does not share, so what only sharing uses is locked.
  val clientMode = client.phase != ClientPhase.Idle
  // Without a SIM card there is no mobile data to share. Looked at again every few seconds: a SIM put in (or an
  // eSIM switched on) while the app is open turns "Paylaşımı başlat" on without leaving it.
  var simCard by remember { mutableStateOf(SimCard.present(context)) }
  // Where the shared internet comes from: mobile data or the Wi-Fi the phone is on. A running sharing keeps its own.
  var source by remember { mutableStateOf(sourcePreference(context)) }
  LaunchedEffect(Unit) {
    while (true) {
      delay(SIM_LOOK_MILLIS)
      simCard = SimCard.present(context)
    }
  }
  val devices = remember(peers, usage, deviceTick, activeNames, computers) {
    peers.map { name ->
      DeviceView(
        name = name,
        computer = name in computers,
        connected = name in activeNames,
        pause = Controls.pause(context, name),
        usedToday = usage.devices[name] ?: 0,
        limitMb = Controls.deviceLimitMb(context, name),
        speedKbps = Controls.deviceSpeedKbps(context, name),
        quiet = Controls.quietHours(context, name),
        guest = Guests.note(context, name),
      )
    }
  }
  // Four sections and a bar to move between them; each section keeps its own scroll position while open.
  val scroll = rememberSaveable(tab, saver = ScrollState.Saver) { ScrollState(0) }
  Scaffold(
    containerColor = MaterialTheme.colorScheme.background,
    bottomBar = { SectionBar(tab, sharing = state.running, onSelect = { tab = it }) },
  ) { inner ->
    Box(modifier = Modifier.fillMaxSize().padding(inner).consumeWindowInsets(inner), contentAlignment = Alignment.TopCenter) {
      Column(
        modifier = Modifier.widthIn(max = 560.dp).fillMaxWidth().verticalScroll(scroll).padding(horizontal = 20.dp).padding(top = 8.dp, bottom = 24.dp),
      ) {
        Header(tab)
        when (tab) {
          Tab.Share -> {
            Reveal(!ignoringBattery) {
              Banner(
                tone = Tone.Warning,
                icon = AppIcons.Alert,
                text = "Ekran kapalıyken Android paylaşımı kesebilir.",
                actionLabel = "Düzelt",
                onAction = { requestIgnoreBattery(context) },
              )
            }
            // A paired device brought a newer release: this phone takes it too (Android asks before installing).
            Reveal(state.pendingApp != null) {
              state.pendingApp?.let { apk ->
                Banner(
                  tone = Tone.Info,
                  icon = AppIcons.Download,
                  text = "AleShare ${apk.name} hazır. Kurulurken paylaşım birkaç saniye kesilir, sonra kendiliğinden devam eder.",
                  actionLabel = "Kur",
                  onAction = { installUpdate(context, apk.path) },
                )
              }
            }
            OnlineUpdateBanner(alreadyHere = state.pendingApp?.code)
            Box(modifier = Modifier.padding(bottom = 12.dp)) {
              SharePanel(
                state,
                blocked = clientMode,
                source = if (state.running || state.starting) state.source else source,
                sourceProblem = sourceProblem(source, simCard),
                onSource = { source = it; setSourcePreference(context, it) },
                connected = connectedCount,
                history = rateHistory,
                onToggle = { if (state.running || state.starting) GatewayService.stop(context) else begin() },
              )
            }
            Reveal(state.running && ownNetwork) {
              NetworkPanel(
                wifi = wifi,
                change = state.wifiChange,
                showPassword = showPassword,
                onTogglePassword = { showPassword = !showPassword },
                onCopy = { label, text, sensitive -> copyText(context, label, text, sensitive) },
                onShowQr = { wifiQr = qrBitmap(WifiNetwork.qrText(wifi)).asImageBitmap() },
                onChange = { changingWifi = true },
                onSwitchNow = { GatewayService.switchWifiNow(context) },
                onCancelChange = { GatewayService.cancelWifiChange(context) },
              )
            }
            DevicesPanel(
              devices = devices,
              sharing = state.running,
              onAdd = { showSheet = true },
              onOpen = { deviceSheet = it },
              locked = clientMode,
            )
          }

          Tab.Data -> {
            Box(modifier = Modifier.padding(bottom = 12.dp)) {
              TodayPanel(
                usage = usage, limitMb = limitMb, onLimit = { choosingLimit = true }, locked = clientMode,
                overWifi = (state.running || state.starting) && state.source == Source.Wifi,
              )
            }
            Box(modifier = Modifier.padding(bottom = 12.dp)) {
              PlanPanel(
                plan = plan,
                days = planDays,
                usageAccess = usageAccess,
                onEdit = { editingPlan = true },
                onAllow = { runCatching { context.startActivity(Intent(Settings.ACTION_USAGE_ACCESS_SETTINGS)) } },
                locked = clientMode,
              )
            }
            Box(modifier = Modifier.padding(bottom = 12.dp)) {
              SpeedPanel(speed, onStart = { SpeedTest.start(context) })
            }
            Reveal(traffic != null) { traffic?.let { SessionPanel(it) } }
          }

          Tab.Settings -> {
            SettingsContent(
              saveData = saveData,
              priority = priority,
              splitTls = splitTls,
              onSaveData = {
                saveData = it
                Controls.setSaveData(context, it)
                core.setSaveData(it)
              },
              onPriority = {
                priority = it
                Controls.setPriority(context, it)
                core.setPriority(it)
              },
              onSplit = {
                splitTls = it
                setSplitPreference(context, it)
                core.setSplitTLS(it) // at once for the connections that start from now on, running or not
              },
              guard = guard,
              onGuard = {
                guard = it
                Controls.setGuard(context, it) // the service lifts or applies the slowdown on its next look
              },
              idleMin = idleMin,
              onIdle = { choosingIdle = true },
              band5 = band5,
              // Moving a running network to the other band drops every device for a while: only while sharing is off.
              bandLocked = state.running || state.starting,
              onBand5 = {
                band5 = it
                Controls.setBand5(context, it)
              },
              locked = clientMode,
            )
            Footer()
          }

          Tab.Connect -> {
            // The connection runs in the background as a VPN: Android's battery limits may cut it while the screen
            // is off, as they would sharing.
            Reveal(savedJoin != null && !ignoringBattery) {
              Banner(
                tone = Tone.Warning,
                icon = AppIcons.Alert,
                text = "Ekran kapalıyken Android bağlantıyı kesebilir.",
                actionLabel = "Düzelt",
                onAction = { requestIgnoreBattery(context) },
              )
            }
            OnlineUpdateBanner(alreadyHere = null)
            ClientPanels(
              saved = savedJoin,
              state = client,
              sharing = state.running || state.starting,
              supported = Build.VERSION.SDK_INT >= 29,
              onScan = {
                scan { link ->
                    val wifiCode = WifiNetwork.parseQr(link)
                    when {
                      wifiCode == null -> handleLink(link, onJoin = { pendingJoin = it; pendingLink = link }, onProblem = { problem = it })
                      // The phone's network got a new name or password while this device was away: its Wi-Fi code is enough.
                      savedJoin != null -> {
                        ClientStore.updateWifi(context, wifiCode)?.let { savedJoin = it }
                        ClientStore.setNext(context, null)
                        ClientService.reload(context)
                        Toast.makeText(context, "Wi-Fi ağı güncellendi: ${wifiCode.ssid}", Toast.LENGTH_SHORT).show()
                      }
                      else -> problem = "Bu yalnızca bir Wi-Fi kodu. Önce paylaşan telefonda Cihazlar → Ekle → Telefon veya tablet ile çıkan AleShare kodunu okut."
                    }
                }
              },
              onPaste = { showPaste = true },
              onConnect = connect,
              onStop = { ClientService.stop(context) },
              onForget = {
                ClientStore.clear(context)
                savedJoin = null
              },
              onVpnSettings = { openSettings(context, Intent(Settings.ACTION_VPN_SETTINGS)) },
              onDownloadUpdate = { ClientService.downloadUpdate(context) },
              onInstallUpdate = { installUpdate(context, it) },
            )
          }
        }
      }
    }
  }


  if (choosingIdle) {
    IdleStopDialog(
      current = idleMin,
      onPick = {
        Controls.setIdleStopMin(context, it)
        idleMin = it
        choosingIdle = false
      },
      onDismiss = { choosingIdle = false },
    )
  }

  if (editingPlan) {
    PlanDialog(
      currentMb = Plan.limitMb(context),
      currentDay = Plan.renewDay(context),
      onSave = { mb, day ->
        Plan.set(context, mb, day)
        editingPlan = false
        planTick++
      },
      onDismiss = { editingPlan = false },
    )
  }

  if (addingGuest) {
    GuestDialog(
      taken = peers,
      onCreate = { name, hours, mb ->
        addingGuest = false
        val endpoint = deviceEndpoint(ownNetwork && chosenIp == null, selected?.ip, state.port)
        try {
          val config = core.addPeer(name, endpoint)
          CoreHolder.save(context, core)
          val until = if (hours > 0) System.currentTimeMillis() + hours * 3_600_000L else 0L
          Guests.add(context, name, until, mb)
          peers = peerNames(core)
          val link = if (ownNetwork) runCatching { Wgcore.encodeJoin(wifi.ssid, wifi.passphrase, config) }.getOrNull() else null
          pairing = Pairing(
            name, config, qrBitmap(config).asImageBitmap(), link, link?.let { qrBitmap(it).asImageBitmap() },
            wifiQr = if (ownNetwork) qrBitmap(WifiNetwork.qrText(wifi)).asImageBitmap() else null,
            guestNote = Guests.note(context, name),
          )
        } catch (e: Exception) {
          problem = e.message ?: e.toString()
        }
      },
      onDismiss = { addingGuest = false },
    )
  }

  if (choosingLimit) {
    LimitDialog(
      currentMb = limitMb,
      onPick = {
        DataUsage.setLimitMb(context, it)
        limitMb = it
        choosingLimit = false
      },
      onDismiss = { choosingLimit = false },
    )
  }

  if (showSheet) {
    AddDeviceSheet(
      canScanQr = ownNetwork || selected != null,
      sharing = state.running,
      onComputer = {
        showSheet = false
        scan { text ->
          // The computer keeps asking; from now on the phone admits it through any pairing window it opens.
          runCatching { core.expectComputer(text) }
            .onSuccess { waitingComputer = true }
            .onFailure { problem = "Bu, bilgisayarın eşleşme kodu değil. Bilgisayarda AleShare.exe'nin gösterdiği QR kodu okut." }
        }
      },
      onPhone = {
        showSheet = false
        showName = true
      },
      onGuest = {
        showSheet = false
        addingGuest = true
      },
      onDismiss = { showSheet = false },
    )
  }

  wifiQr?.let { WifiQrDialog(ssid = wifi.ssid, qr = it, onDismiss = { wifiQr = null }) }

  removingTile?.let { name -> RemoveTileDialog(name = name, onDismiss = { removingTile = null }) }

  if (changingWifi && state.running) {
    ChangeWifiDialog(
      current = wifi,
      connected = remember { connectedFollowers(context, core).size },
      onChange = { next ->
        changingWifi = false
        showPassword = false
        GatewayService.changeWifi(context, next)
      },
      onDismiss = { changingWifi = false },
    )
  }

  if (showName) {
    NameDeviceDialog(
      onCreate = { name ->
        val endpoint = deviceEndpoint(ownNetwork && chosenIp == null, selected?.ip, state.port)
        try {
          val config = core.addPeer(name, endpoint)
          CoreHolder.save(context, core)
          peers = peerNames(core)
          // For another phone running this app one code is enough: it carries the Wi-Fi network too.
          val link = if (ownNetwork) runCatching { Wgcore.encodeJoin(wifi.ssid, wifi.passphrase, config) }.getOrNull() else null
          pairing = Pairing(name, config, qrBitmap(config).asImageBitmap(), link, link?.let { qrBitmap(it).asImageBitmap() })
        } catch (e: Exception) {
          problem = e.message ?: e.toString()
        }
        showName = false
      },
      onDismiss = { showName = false },
    )
  }

  pairing?.let { p ->
    QrDialog(
      name = p.name,
      hotspot = p.linkQr,
      wireGuard = p.qr,
      wifi = p.wifiQr,
      guestNote = p.guestNote,
      onSave = { saveFile.launch("${p.name}.conf") },
      onShare = {
        val send = Intent(Intent.ACTION_SEND).setType("text/plain").putExtra(Intent.EXTRA_TEXT, p.config)
        context.startActivity(Intent.createChooser(send, "WireGuard yapılandırması"))
      },
      onCopyLink = { p.link?.let { copyText(context, "Bağlantı", it, sensitive = true) } },
      onDismiss = { pairing = null },
    )
  }

  pendingJoin?.let { join ->
    JoinConfirmDialog(
      join = join,
      onConfirm = {
        val link = pendingLink
        pendingJoin = null
        pendingLink = null
        if (state.running || state.starting) {
          // One or the other: a phone that shares its internet cannot take another phone's at the same time.
          problem = "Bu telefon şu an internetini paylaşıyor. Başka bir telefona bağlanmak için önce paylaşımı durdur, sonra kodu yeniden okut."
        } else {
          link?.let { ClientStore.save(context, it) }
          savedJoin = join
          // A connection that runs goes over to the new phone; one that does not is started.
          if (client.phase != ClientPhase.Idle) ClientService.reload(context) else connect()
        }
      },
      onDismiss = {
        pendingJoin = null
        pendingLink = null
      },
    )
  }

  if (showPaste) {
    PasteLinkDialog(
      initial = clipboardText(context)?.takeIf { it.startsWith("hotspot://") }.orEmpty(),
      onUse = { text ->
        showPaste = false
        handleLink(text, onJoin = { pendingJoin = it; pendingLink = text.trim() }, onProblem = { problem = it })
      },
      onDismiss = { showPaste = false },
    )
  }

  pairingRequest?.let { PairingRequestDialog(it) }

  if (waitingComputer) ComputerWaitDialog(onDismiss = { waitingComputer = false })

  deviceSheet?.let { name ->
    devices.firstOrNull { it.name == name }?.let { device ->
      DeviceSheet(
        device = device,
        sharing = state.running,
        onPause = { pause ->
          Controls.setPaused(context, CoreHolder.peek(), name, pause)
          deviceTick++
        },
        onLimit = { mb ->
          Controls.setDeviceLimitMb(context, name, mb)
          deviceTick++
        },
        onSpeed = { kbps ->
          Controls.setDeviceSpeedKbps(context, CoreHolder.peek(), name, kbps)
          deviceTick++
        },
        onQuiet = { q ->
          Controls.setQuietHours(context, CoreHolder.peek(), name, q)
          deviceTick++
        },
        onRemove = {
          deviceSheet = null
          removing = name
        },
        onDismiss = { deviceSheet = null },
      )
    } ?: LaunchedEffect(name) { deviceSheet = null } // removed meanwhile (by a computer, or as a guest whose time ran out)
  }

  removing?.let { name ->
    RemoveDeviceDialog(
      name = name,
      onRemove = {
        core.removePeer(name)
        CoreHolder.save(context, core)
        Guests.forget(context, name)
        Controls.forget(context, name) // its limits and pauses: the next device of that name starts without them
        Updates.renewToken(context, core) // the removed device knew the store's token
        peers = peerNames(core)
        removing = null
      },
      onDismiss = { removing = null },
    )
  }

  problem?.let { ProblemDialog(it) { problem = null } }
}

@Composable
private fun Header(tab: Tab) {
  Row(modifier = Modifier.padding(top = 14.dp, bottom = 16.dp), verticalAlignment = Alignment.CenterVertically) {
    if (tab == Tab.Share) {
      // The logo (the hotspot sign), in the app's colour: drawn by core/cmd/hotspot-pc/winres/icon_gen.go.
      Icon(painterResource(R.drawable.ic_logo), contentDescription = null, modifier = Modifier.size(28.dp), tint = MaterialTheme.colorScheme.primary)
      Spacer(Modifier.width(10.dp))
    }
    Text(tab.title, style = MaterialTheme.typography.titleLarge, color = MaterialTheme.colorScheme.onSurface)
  }
}

/** The bar at the bottom: the four sections; a dot on Paylaş while sharing runs. */
@Composable
private fun SectionBar(selected: Tab, sharing: Boolean, onSelect: (Tab) -> Unit) {
  NavigationBar(containerColor = MaterialTheme.colorScheme.surface, tonalElevation = 0.dp) {
    Tab.entries.forEach { tab ->
      NavigationBarItem(
        selected = tab == selected,
        onClick = { onSelect(tab) },
        icon = {
          val icon = when (tab) {
            Tab.Share -> AppIcons.Toggle
            Tab.Data -> AppIcons.Chart
            Tab.Settings -> AppIcons.Sliders
            Tab.Connect -> AppIcons.Link
          }
          BadgedBox(badge = { if (tab == Tab.Share && sharing) Badge(containerColor = HotspotTheme.status.ok) }) {
            Icon(icon, contentDescription = null, modifier = Modifier.size(22.dp))
          }
        },
        label = { Text(tab.label, style = MaterialTheme.typography.labelMedium) },
      )
    }
  }
}

/** A scanned or pasted text: a join link, or something else. */
private fun handleLink(text: String, onJoin: (Join) -> Unit, onProblem: (String) -> Unit) {
  try {
    onJoin(Wgcore.parseJoin(text.trim()))
  } catch (e: Exception) {
    onProblem("Bu bağlantı AleShare'e ait değil ya da bozuk.")
  }
}

/** The system's QR scanner (Google Play services): needs no camera permission of ours. [onUnavailable] when it cannot run. */
private fun scanLink(context: Context, onLink: (String) -> Unit, onUnavailable: () -> Unit) {
  val options = GmsBarcodeScannerOptions.Builder().setBarcodeFormats(Barcode.FORMAT_QR_CODE).build()
  runCatching {
    GmsBarcodeScanning.getClient(context, options).startScan()
      .addOnSuccessListener { barcode -> barcode.rawValue?.let(onLink) }
      .addOnFailureListener { onUnavailable() } // no Google Play services, or their scanner cannot run here
  }.onFailure { onUnavailable() }
}

/**
 * Asks the system to put one of this app's tiles ([service], shown as [label] with [icon]) into Quick Settings; the
 * system shows its own dialog for that. [markAdded] is called when it is in the panel, [done] after any answer.
 */
@RequiresApi(33)
private fun requestAddTile(context: Context, service: Class<*>, label: Int, icon: Int, markAdded: () -> Unit, done: () -> Unit) {
  context.getSystemService(StatusBarManager::class.java).requestAddTileService(
    ComponentName(context, service),
    context.getString(label),
    Icon.createWithResource(context, icon),
    context.mainExecutor,
  ) { result ->
    val added = result == StatusBarManager.TILE_ADD_REQUEST_RESULT_TILE_ADDED || result == StatusBarManager.TILE_ADD_REQUEST_RESULT_TILE_ALREADY_ADDED
    if (added) markAdded()
    done()
    // The user saying no needs no answer; anything else is a failure worth explaining.
    if (!added && result != StatusBarManager.TILE_ADD_REQUEST_RESULT_TILE_NOT_ADDED) {
      Toast.makeText(context, "Eklenemedi. Bildirim panelinde düzenle simgesinden elle ekleyebilirsin.", Toast.LENGTH_LONG).show()
    }
  }
}

private fun clipboardText(context: Context): String? =
  context.getSystemService(ClipboardManager::class.java).primaryClip?.takeIf { it.itemCount > 0 }?.getItemAt(0)?.text?.toString()

/** A setting that is on or off: its name, a few words under it, and the switch; the whole row switches it. */
@Composable
private fun SwitchRow(title: String, caption: String?, checked: Boolean, onChange: (Boolean) -> Unit, enabled: Boolean = true) {
  Row(
    modifier = Modifier.fillMaxWidth().heightIn(min = 56.dp).alpha(if (enabled) 1f else 0.38f)
      .toggleable(value = checked, enabled = enabled, role = Role.Switch, onValueChange = onChange)
      .padding(vertical = 10.dp),
    verticalAlignment = Alignment.CenterVertically,
  ) {
    Column(modifier = Modifier.weight(1f)) {
      Text(title, style = MaterialTheme.typography.bodyLarge, color = MaterialTheme.colorScheme.onSurface)
      caption?.let { Caption(it) }
    }
    Spacer(Modifier.width(16.dp))
    Switch(checked = checked, onCheckedChange = null, enabled = enabled)
  }
}

/** A setting with a value to pick, or something to do: its name on the left, the value on the right. */
@Composable
private fun ValueRow(title: String, value: String, onClick: () -> Unit, enabled: Boolean = true) {
  Row(
    modifier = Modifier.fillMaxWidth().heightIn(min = 56.dp).alpha(if (enabled) 1f else 0.38f)
      .clickable(enabled = enabled, role = Role.Button, onClick = onClick).padding(vertical = 10.dp),
    verticalAlignment = Alignment.CenterVertically,
  ) {
    Text(title, modifier = Modifier.weight(1f), style = MaterialTheme.typography.bodyLarge, color = MaterialTheme.colorScheme.onSurface)
    Spacer(Modifier.width(16.dp))
    Text(value, style = MaterialTheme.typography.bodyMedium, color = MaterialTheme.colorScheme.onSurfaceVariant)
  }
}

@Composable
private fun RowDivider() = HorizontalDivider(color = MaterialTheme.colorScheme.outlineVariant)

/** A group of settings: a small name above, its rows in one quiet card. */
@Composable
private fun SettingsGroup(title: String, content: @Composable ColumnScope.() -> Unit) {
  Text(
    title.uppercase(Tr.locale),
    modifier = Modifier.padding(start = 4.dp, bottom = 6.dp),
    style = MaterialTheme.typography.labelMedium,
    color = MaterialTheme.colorScheme.onSurfaceVariant,
  )
  Surface(shape = MaterialTheme.shapes.large, color = MaterialTheme.colorScheme.surface, modifier = Modifier.fillMaxWidth()) {
    Column(modifier = Modifier.padding(horizontal = 18.dp, vertical = 4.dp), content = content)
  }
  Spacer(Modifier.height(24.dp))
}

/**
 * What there is to set, in four short groups: the shared traffic, the phone, the Wi-Fi network and updates. Each row
 * says in a few words what it does; the rest of the app explains more where it matters.
 */
@Composable
private fun SettingsContent(
  saveData: Boolean,
  priority: Boolean,
  splitTls: Boolean,
  onSaveData: (Boolean) -> Unit,
  onPriority: (Boolean) -> Unit,
  onSplit: (Boolean) -> Unit,
  guard: Boolean,
  onGuard: (Boolean) -> Unit,
  idleMin: Long,
  onIdle: () -> Unit,
  band5: Boolean,
  bandLocked: Boolean,
  onBand5: (Boolean) -> Unit,
  locked: Boolean,
) {
  // A device that joined another phone does not share: what only sharing uses is locked until it stops.
  if (locked) {
    Banner(Tone.Info, AppIcons.Link, "Bu cihaz şu an başka bir telefona bağlı. Paylaşım ayarları, bağlantıyı durdurunca açılır.")
    Spacer(Modifier.height(16.dp))
  }
  SettingsGroup("Paylaşım") {
    SwitchRow("Veri tasarrufu", "Sistem ve oyun güncellemeleri indirilmez", saveData, onSaveData, enabled = !locked)
    RowDivider()
    SwitchRow("Akıllı öncelik", "Görüşme ve oyunlar önce gelir", priority, onPriority, enabled = !locked)
    RowDivider()
    SwitchRow("AleDPI modu", "Açılmayan siteleri açmayı dener", splitTls, onSplit, enabled = !locked)
  }

  SettingsGroup("Telefon") {
    SwitchRow("Pil ve ısı koruması", "Isınınca yavaşlar, pil %${Guard.BATTERY_STOP}'e düşünce durur", guard, onGuard, enabled = !locked)
    RowDivider()
    ValueRow("Kimse bağlı değilken kapat", if (idleMin > 0) idleLabel(idleMin) + " sonra" else "Kapalı", onIdle, enabled = !locked)
  }

  SettingsGroup("Wi-Fi ağı") {
    Row(modifier = Modifier.fillMaxWidth().heightIn(min = 56.dp).padding(vertical = 10.dp), verticalAlignment = Alignment.CenterVertically) {
      Column(modifier = Modifier.weight(1f)) {
        Text("Bant", style = MaterialTheme.typography.bodyLarge, color = MaterialTheme.colorScheme.onSurface)
        Caption(
          when {
            bandLocked -> "Paylaşım açıkken değiştirilemez"
            band5 -> "Daha hızlı"
            else -> "Her cihaz görür"
          }
        )
      }
      Spacer(Modifier.width(16.dp))
      SegmentedSwitch(
        options = listOf(false to "2,4 GHz", true to "5 GHz"), selected = band5, onSelect = { if (it != band5) onBand5(it) }, enabled = !locked && !bandLocked,
        modifier = Modifier.width(176.dp),
      )
    }
  }

  SettingsGroup("Güncelleme") {
    val context = LocalContext.current
    val online by OnlineUpdate.state.collectAsStateWithLifecycle()
    var auto by remember { mutableStateOf(OnlineUpdate.enabled(context)) }
    SwitchRow("Yeni sürümleri denetle", "Yeni sürüm çıkınca haber verir", auto, { auto = it; OnlineUpdate.setEnabled(context, it) })
    RowDivider()
    ValueRow(
      "Şimdi denetle",
      when {
        online.checking -> "Bakılıyor…"
        online.offer != null -> "${online.offer?.name} çıktı"
        else -> ""
      },
      onClick = {
        OnlineUpdate.check(context, force = true) { answered ->
          Handler(Looper.getMainLooper()).post {
            val offer = OnlineUpdate.state.value.offer
            val text = when {
              // While this device takes another phone's internet, the app's own connections stay outside the
              // tunnel (they are the tunnel's way to that phone), so the app itself cannot reach GitHub.
              !answered && ClientState.state.value.phase != ClientPhase.Idle ->
                "Bu cihaz internetini başka bir telefondan alırken GitHub'a kendisi bakamaz. Yeni sürüm, paylaşan telefon güncellenince ondan gelir."
              !answered -> "GitHub'a ulaşılamadı. İnternet bağlantına bak."
              offer != null -> "AleShare ${offer.name} yayımlandı. Paylaş sekmesinden indirebilirsin."
              else -> "AleShare güncel."
            }
            Toast.makeText(context, text, Toast.LENGTH_LONG).show()
          }
        }
      },
    )
  }
}

/** The line under a device's name: why it is paused, or what it used today and the rules it has. */
/** "30 dk", "1 saat", "2 saat". */
internal fun idleLabel(minutes: Long): String = if (minutes >= 60 && minutes % 60 == 0L) "${minutes / 60} saat" else "$minutes dk"

@Composable
private fun Footer() {
  val context = LocalContext.current
  // Which release this is: the number of the APK file, to tell whether a device has the newest one.
  val version = remember { runCatching { context.packageManager.getPackageInfo(context.packageName, 0).versionName }.getOrNull() }
  Column(horizontalAlignment = Alignment.CenterHorizontally, modifier = Modifier.fillMaxWidth()) {
    Row(
      modifier = Modifier.fillMaxWidth().padding(horizontal = 8.dp),
      horizontalArrangement = Arrangement.spacedBy(10.dp, Alignment.CenterHorizontally),
      verticalAlignment = Alignment.Top,
    ) {
      Icon(AppIcons.Shield, contentDescription = null, modifier = Modifier.size(16.dp).padding(top = 2.dp), tint = MaterialTheme.colorScheme.onSurfaceVariant)
      Caption("Bağlantı WireGuard ile şifrelenir. Girilen siteler kaydedilmez.", Modifier.weight(1f, fill = false))
    }
    Spacer(Modifier.height(10.dp))
    version?.let { Caption("Sürüm $it") }
  }
}

private fun peerNames(core: Core): List<String> = core.peerNames().split("\n").filter { it.isNotBlank() }

private fun isIgnoringBattery(context: Context): Boolean =
  context.getSystemService(PowerManager::class.java).isIgnoringBatteryOptimizations(context.packageName)



private fun requestIgnoreBattery(context: Context) {
  openSettings(
    context,
    Intent(Settings.ACTION_REQUEST_IGNORE_BATTERY_OPTIMIZATIONS, Uri.parse("package:${context.packageName}")),
    Intent(Settings.ACTION_IGNORE_BATTERY_OPTIMIZATION_SETTINGS),
  )
}

/**
 * A newer release is out on GitHub: say so, download it when the user asks, then let Android install it.
 * [alreadyHere] is the release a paired device brought, which has a banner of its own.
 */
@Composable
private fun OnlineUpdateBanner(alreadyHere: Long?) {
  val context = LocalContext.current
  val online by OnlineUpdate.state.collectAsStateWithLifecycle()
  val offer = online.offer?.takeIf { it.code > (alreadyHere ?: 0L) }
  Reveal(offer != null) {
    offer?.let { r ->
      val file = online.file
      val pct = online.progress
      val error = online.error
      when {
        error != null -> Banner(Tone.Error, AppIcons.Alert, error, actionLabel = "Yeniden dene", onAction = { OnlineUpdate.download(context) })
        file != null -> Banner(
          Tone.Info, AppIcons.Download,
          "AleShare ${r.name} indirildi. Kurulurken paylaşım birkaç saniye kesilir, sonra kendiliğinden devam eder.",
          actionLabel = "Kur", onAction = { installUpdate(context, file) },
        )
        pct != null -> Banner(Tone.Info, AppIcons.Download, "AleShare ${r.name} indiriliyor: %$pct")
        else -> Banner(
          Tone.Info, AppIcons.Download, "AleShare ${r.name} yayımlandı (${humanBytes(r.size)}).",
          actionLabel = "İndir", onAction = { OnlineUpdate.download(context) },
        )
      }
    }
  }
}

/**
 * Hands the APK taken from the phone to Android's installer, which asks the user and takes it only when it is
 * signed like this app. The first time, Android wants the user to allow this app to install apps at all.
 */
private fun installUpdate(context: Context, path: String) {
  if (!context.packageManager.canRequestPackageInstalls()) {
    openSettings(context, Intent(Settings.ACTION_MANAGE_UNKNOWN_APP_SOURCES, Uri.parse("package:${context.packageName}")))
    Toast.makeText(context, "AleShare'e izin ver, sonra geri dönüp Kur'a dokun.", Toast.LENGTH_LONG).show()
    return
  }
  val uri = FileProvider.getUriForFile(context, "${context.packageName}.files", File(path))
  val intent = Intent(Intent.ACTION_VIEW)
    .setDataAndType(uri, "application/vnd.android.package-archive")
    .addFlags(Intent.FLAG_GRANT_READ_URI_PERMISSION or Intent.FLAG_ACTIVITY_NEW_TASK)
  runCatching { context.startActivity(intent) }
    .onFailure { Toast.makeText(context, "Kurulum açılamadı: ${it.message ?: it}", Toast.LENGTH_LONG).show() }
}

/** Android 13+ shows its own "copied" confirmation and hides a sensitive clip from the preview; older versions get a toast. */
private fun copyText(context: Context, label: String, text: String, sensitive: Boolean) {
  val clip = ClipData.newPlainText(label, text)
  if (sensitive && Build.VERSION.SDK_INT >= 33) {
    clip.description.extras = PersistableBundle().apply { putBoolean(ClipDescription.EXTRA_IS_SENSITIVE, true) }
  }
  context.getSystemService(ClipboardManager::class.java).setPrimaryClip(clip)
  if (Build.VERSION.SDK_INT < 33) Toast.makeText(context, "$label kopyalandı", Toast.LENGTH_SHORT).show()
}

/**
 * Opens one of Android's settings screens, or the next one given when a device has no such screen (some makers and
 * work profiles leave them out); says so when there is none at all instead of ending the app.
 */
private fun openSettings(context: Context, vararg intents: Intent) {
  for (intent in intents) {
    if (runCatching { context.startActivity(intent) }.isSuccess) return
  }
  Toast.makeText(context, "Bu ayar ekranı bu cihazda yok.", Toast.LENGTH_LONG).show()
}
