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
import androidx.compose.material3.Switch
import androidx.compose.material3.Text
import androidx.compose.material3.TextButton
import androidx.compose.runtime.Composable
import androidx.compose.runtime.LaunchedEffect
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.remember
import androidx.compose.runtime.rememberUpdatedState
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
import dev.hotspot.app.MAKER
import dev.hotspot.app.OnlineUpdate
import dev.hotspot.app.QuietHours
import dev.hotspot.app.Tr
import dev.hotspot.app.bitRate
import dev.hotspot.app.bottleneck

private const val PAIRING_REOPEN_MILLIS = 30_000L
private const val ACTIVE_PEER_SECONDS = 75L
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

/** Share the internet of this phone, or use the internet of another phone. */
/** The app's sections, in the bar at the bottom: sharing, data, settings, and joining another phone. */
private enum class Tab(val title: String, val label: String) {
  Share("AleShare", "Paylaş"),
  Data("Veri", "Veri"),
  Settings("Ayarlar", "Ayarlar"),
  Connect("Bağlan", "Bağlan"),
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
  var rates by remember { mutableStateOf(0L to 0L) } // bytes per second now, down and up
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
  var pairing by remember { mutableStateOf<Pairing?>(null) }
  var removing by remember { mutableStateOf<String?>(null) }
  var problem by remember { mutableStateOf<String?>(null) }
  var ownNetwork by remember { mutableStateOf(ownNetworkPreference(context)) }
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
  // Read again when the network changes: the user may give it a new name or password.
  val wifi = remember(state.network) { WifiNetwork.credentials(context) }

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
          connectedCount = if (running) core.activePeers(ACTIVE_PEER_SECONDS).lines().count { it.isNotBlank() } else 0
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
      return@LaunchedEffect
    }
    lifecycleOwner.repeatOnLifecycle(Lifecycle.State.RESUMED) {
      var before: TrafficStats? = null
      while (true) {
        val now = core.stats().toTraffic()
        before?.let { rates = (now.bytesDown - it.bytesDown).coerceAtLeast(0) to (now.bytesUp - it.bytesUp).coerceAtLeast(0) }
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
        if (ownNetwork) add(if (Build.VERSION.SDK_INT >= 33) Manifest.permission.NEARBY_WIFI_DEVICES else Manifest.permission.ACCESS_FINE_LOCATION)
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
              StatusPanel(
                state,
                blocked = clientMode,
                connected = connectedCount,
                down = rates.first,
                up = rates.second,
                onToggle = { if (state.running || state.starting) GatewayService.stop(context) else begin() },
              )
            }
            Reveal(state.running && ownNetwork) {
              WifiPanel(
                wifi = wifi,
                band = state.band,
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
              peers = peers,
              notes = remember(peers, usage, deviceTick) { peers.associateWith { deviceNote(context, it, usage.devices[it] ?: 0) } },
              sharing = state.running,
              onAdd = { showSheet = true },
              onRemove = { removing = it },
              onOpen = { deviceSheet = it },
              locked = clientMode,
            )
          }

          Tab.Data -> {
            Box(modifier = Modifier.padding(bottom = 12.dp)) {
              UsagePanel(usage = usage, limitMb = limitMb, onLimit = { choosingLimit = true }, locked = clientMode)
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
            Reveal(traffic != null) { traffic?.let { TrafficPanel(it) } }
          }

          Tab.Settings -> {
            SettingsContent(
              blockAds = blockAds,
              saveData = saveData,
              priority = priority,
              splitTls = splitTls,
              blocked = traffic?.dnsBlocked,
              splitCount = traffic?.tcpSplit,
              onBlockAds = {
                blockAds = it
                Controls.setBlockAds(context, it)
                core.setBlockAds(it)
              },
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
              ownNetwork = ownNetwork,
              ownNetworkEditable = !state.running && !state.starting,
              onOwnNetwork = {
                ownNetwork = it
                setOwnNetworkPref(context, it)
              },
              band5 = band5,
              onBand5 = {
                band5 = it
                Controls.setBand5(context, it)
                GatewayService.changeBand(context) // a running network moves at once; devices rejoin by themselves
              },
              tileAdded = tileAdded,
              onAddTile = if (Build.VERSION.SDK_INT >= 33) ({
                requestAddTile(context, ShareTileService::class.java, R.string.tile_label, R.drawable.ic_hotspot,
                  markAdded = { ShareTileService.setAdded(context, true) }, done = { tileAdded = ShareTileService.added(context) })
              }) else null,
              onRemoveTile = { removingTile = "Paylaşım" },
              addressesOpen = advanced,
              onToggleAddresses = { advanced = !advanced },
              addresses = addresses,
              selected = selected,
              port = state.port,
              onSelect = { chosenIp = it },
              locked = clientMode,
            )
            Footer()
          }

          Tab.Connect -> {
            OnlineUpdateBanner(alreadyHere = null)
            ClientPanels(
              saved = savedJoin,
              state = client,
              sharing = state.running || state.starting,
              supported = Build.VERSION.SDK_INT >= 29,
              onScan = {
                scanLink(
                  context,
                  onLink = { link ->
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
                  },
                  onProblem = { problem = it },
                )
              },
              onPaste = { showPaste = true },
              onConnect = connect,
              onStop = { ClientService.stop(context) },
              onForget = {
                ClientStore.clear(context)
                savedJoin = null
              },
              onVpnSettings = { context.startActivity(Intent(Settings.ACTION_VPN_SETTINGS)) },
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
        val endpoint = "${selected?.ip}:${state.port}"
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
      canScanQr = selected != null,
      sharing = state.running,
      onComputer = {
        showSheet = false
        scanLink(
          context,
          onLink = { text ->
            // The computer keeps asking; from now on the phone admits it through any pairing window it opens.
            runCatching { core.expectComputer(text) }
              .onSuccess { waitingComputer = true }
              .onFailure { problem = "Bu, bilgisayarın eşleşme kodu değil. Bilgisayarda AleShare.exe'nin gösterdiği QR kodu okut." }
          },
          onProblem = { problem = it },
        )
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
        val endpoint = "${selected?.ip}:${state.port}"
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
        pendingLink?.let { ClientStore.save(context, it) }
        savedJoin = join
        pendingJoin = null
        pendingLink = null
        connect()
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
    DeviceDialog(
      name = name,
      usedToday = usage.devices[name] ?: 0,
      limitMb = remember(name, deviceTick) { Controls.deviceLimitMb(context, name) },
      paused = remember(name, deviceTick) { Controls.pause(context, name) != Controls.Pause.None },
      onPause = { pause ->
        Controls.setPaused(context, CoreHolder.peek(), name, pause)
        deviceTick++
      },
      onLimit = { mb ->
        Controls.setDeviceLimitMb(context, name, mb)
        deviceTick++
      },
      speedKbps = remember(name, deviceTick) { Controls.deviceSpeedKbps(context, name) },
      onSpeed = { kbps ->
        Controls.setDeviceSpeedKbps(context, CoreHolder.peek(), name, kbps)
        deviceTick++
      },
      quiet = remember(name, deviceTick) { Controls.quietHours(context, name) },
      onQuiet = { q ->
        Controls.setQuietHours(context, CoreHolder.peek(), name, q)
        deviceTick++
      },
      guestNote = remember(name, deviceTick) { Guests.note(context, name) },
      onRemove = {
        deviceSheet = null
        removing = name
      },
      onDismiss = { deviceSheet = null },
    )
  }

  removing?.let { name ->
    RemoveDeviceDialog(
      name = name,
      onRemove = {
        core.removePeer(name)
        CoreHolder.save(context, core)
        Guests.forget(context, name)
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

/** The system's QR scanner (Google Play services): needs no camera permission of ours. */
private fun scanLink(context: Context, onLink: (String) -> Unit, onProblem: (String) -> Unit) {
  val options = GmsBarcodeScannerOptions.Builder().setBarcodeFormats(Barcode.FORMAT_QR_CODE).build()
  GmsBarcodeScanning.getClient(context, options).startScan()
    .addOnSuccessListener { barcode -> barcode.rawValue?.let(onLink) }
    .addOnFailureListener { onProblem("QR tarayıcı açılamadı. Bağlantıyı yapıştırmayı dene.") }
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

/** A Quick Settings tile of the app: added to the panel from here, and taken out again. */
@Composable
private fun TileRow(
  icon: ImageVector,
  title: String,
  addedCaption: String,
  freeCaption: String,
  added: Boolean,
  onAdd: () -> Unit,
  onRemove: () -> Unit,
  enabled: Boolean = true,
) {
  Row(modifier = Modifier.alpha(if (enabled) 1f else 0.38f), verticalAlignment = Alignment.CenterVertically) {
    Box(
      modifier = Modifier.size(40.dp).background(MaterialTheme.colorScheme.primaryContainer, CircleShape),
      contentAlignment = Alignment.Center,
    ) {
      Icon(icon, contentDescription = null, modifier = Modifier.size(20.dp), tint = MaterialTheme.colorScheme.onPrimaryContainer)
    }
    Spacer(Modifier.width(14.dp))
    Column(modifier = Modifier.weight(1f)) {
      Text(title, style = MaterialTheme.typography.bodyLarge, color = MaterialTheme.colorScheme.onSurface)
      Caption(if (added) addedCaption else freeCaption)
    }
    Spacer(Modifier.width(12.dp))
    val shape = MaterialTheme.shapes.medium
    val padding = PaddingValues(horizontal = 18.dp)
    if (added) {
      OutlinedButton(onClick = onRemove, enabled = enabled, modifier = Modifier.height(38.dp), shape = shape, contentPadding = padding) {
        Text("Kaldır", style = MaterialTheme.typography.labelLarge)
      }
    } else {
      FilledTonalButton(onClick = onAdd, enabled = enabled, modifier = Modifier.height(38.dp), shape = shape, contentPadding = padding) {
        Text("Ekle", style = MaterialTheme.typography.labelLarge)
      }
    }
  }
}

private fun clipboardText(context: Context): String? =
  context.getSystemService(ClipboardManager::class.java).primaryClip?.takeIf { it.itemCount > 0 }?.getItemAt(0)?.text?.toString()

/**
 * The one thing the app does, and the one button that does it; while sharing, how fast it goes now ([down] and
 * [up] in bytes per second) and how many devices use it.
 */
@Composable
private fun StatusPanel(state: GatewayUiState, blocked: Boolean, connected: Int, down: Long, up: Long, onToggle: () -> Unit) {
  val status = HotspotTheme.status
  val idle = MaterialTheme.colorScheme.onSurfaceVariant
  val accent by animateColorAsState(
    when {
      state.running -> status.ok
      state.starting -> status.warn
      else -> idle
    },
    label = "status",
  )
  Panel {
    Row(verticalAlignment = Alignment.CenterVertically, horizontalArrangement = Arrangement.spacedBy(8.dp)) {
      StatusDot(accent, pulsing = state.starting)
      Text(
        when {
          state.starting -> "Başlatılıyor"
          state.running -> "Açık"
          else -> "Kapalı"
        },
        style = MaterialTheme.typography.labelLarge,
        color = accent,
      )
    }
    Spacer(Modifier.height(12.dp))
    Text(
      when {
        state.starting -> "Hazırlanıyor"
        state.running -> "Paylaşım açık"
        else -> "İnternetini paylaş"
      },
      style = MaterialTheme.typography.headlineSmall,
      color = MaterialTheme.colorScheme.onSurface,
    )
    Spacer(Modifier.height(6.dp))
    Text(
      when {
        state.starting -> "Mobil veri ve Wi-Fi hazırlanıyor."
        state.running && !state.cellular -> "Mobil veri bekleniyor."
        state.running -> listOfNotNull(if (connected > 0) "$connected cihaz bağlı" else "Bağlı cihaz yok", state.band).joinToString("  ·  ")
        else -> "Bilgisayarın ve tabletin bu telefonun mobil internetini kullanır."
      },
      style = MaterialTheme.typography.bodyMedium,
      color = MaterialTheme.colorScheme.onSurfaceVariant,
    )
    if (state.running) {
      Spacer(Modifier.height(16.dp))
      Row(modifier = Modifier.fillMaxWidth()) {
        Metric(AppIcons.ArrowDown, "İndirme", bitRate(down * 8), Modifier.weight(1f))
        Metric(AppIcons.ArrowUp, "Yükleme", bitRate(up * 8), Modifier.weight(1f))
      }
    }
    state.error?.let {
      Spacer(Modifier.height(14.dp))
      Banner(Tone.Error, AppIcons.Alert, it)
    }
    if (blocked) {
      Spacer(Modifier.height(14.dp))
      Banner(Tone.Info, AppIcons.Alert, "Bu cihaz başka bir telefona bağlıyken paylaşım yapamaz. Önce Bağlan sekmesinden bağlantıyı durdur.")
    }
    Spacer(Modifier.height(20.dp))
    ActionButton(
      text =
        when {
          state.starting -> "Başlatılıyor…"
          state.running -> "Durdur"
          else -> "Paylaşımı başlat"
        },
      onClick = onToggle,
      tonal = state.running,
      enabled = !state.starting && (state.running || !blocked),
    )
  }
}

/** How to join the network the app opened. */
@Composable
private fun WifiPanel(
  wifi: WifiCredentials,
  band: String?,
  change: WifiChange?,
  showPassword: Boolean,
  onTogglePassword: () -> Unit,
  onCopy: (label: String, text: String, sensitive: Boolean) -> Unit,
  onShowQr: () -> Unit,
  onChange: () -> Unit,
  onSwitchNow: () -> Unit,
  onCancelChange: () -> Unit,
) {
  Panel {
    Row(verticalAlignment = Alignment.CenterVertically, horizontalArrangement = Arrangement.spacedBy(10.dp)) {
      Icon(AppIcons.Wifi, contentDescription = null, modifier = Modifier.size(20.dp), tint = MaterialTheme.colorScheme.primary)
      PanelTitle("Wi-Fi ağı")
    }
    Spacer(Modifier.height(8.dp))
    ValueRow("Ağ adı", wifi.ssid, mono = true) {
      IconAction(AppIcons.Copy, "Ağ adını kopyala", onClick = { onCopy("Ağ adı", wifi.ssid, false) })
    }
    HorizontalDivider(modifier = Modifier.padding(vertical = 6.dp), color = MaterialTheme.colorScheme.outlineVariant)
    ValueRow("Parola", if (showPassword) wifi.passphrase else "••••••••••••••••", mono = true) {
      IconAction(if (showPassword) AppIcons.EyeOff else AppIcons.Eye, if (showPassword) "Parolayı gizle" else "Parolayı göster", onClick = onTogglePassword)
      IconAction(AppIcons.Copy, "Parolayı kopyala", onClick = { onCopy("Parola", wifi.passphrase, true) })
    }
    band?.let {
      HorizontalDivider(modifier = Modifier.padding(vertical = 6.dp), color = MaterialTheme.colorScheme.outlineVariant)
      ValueRow("Bant", it)
    }
    Spacer(Modifier.height(10.dp))
    if (change != null) {
      val waiting = change.waitingFor.filterNot { it in change.learned }
      Banner(
        Tone.Info,
        AppIcons.Wifi,
        if (waiting.isEmpty()) "Ağın adı ${change.ssid} oluyor."
        else "Ağın adı ${change.ssid} olacak. Cihazlara haber veriliyor:${change.waitingFor.size - waiting.size}/${change.waitingFor.size} (beklenen: ${waiting.joinToString()})",
      )
      Row(modifier = Modifier.offset(x = (-12).dp)) {
        TextButton(onClick = onSwitchNow) { Text("Şimdi değiştir", style = MaterialTheme.typography.labelLarge) }
        TextButton(onClick = onCancelChange) { Text("Vazgeç", style = MaterialTheme.typography.labelLarge) }
      }
    } else {
      Caption("Bilgisayarı bu ağa bir kez bağlaman yeterli; Windows hatırlar. Telefon ve tabletler QR kodla bağlanır.")
      Spacer(Modifier.height(4.dp))
      Row(modifier = Modifier.offset(x = (-12).dp)) {
        TextButton(onClick = onShowQr) {
          Icon(AppIcons.Qr, contentDescription = null, modifier = Modifier.size(18.dp))
          Spacer(Modifier.width(8.dp))
          Text("QR ile bağlan", style = MaterialTheme.typography.labelLarge)
        }
        TextButton(onClick = onChange) {
          Icon(AppIcons.Edit, contentDescription = null, modifier = Modifier.size(18.dp))
          Spacer(Modifier.width(8.dp))
          Text("Değiştir", style = MaterialTheme.typography.labelLarge)
        }
      }
    }
  }
}

/** The monthly plan: what is used and left of it, what each day may use, and the last 30 days as bars. */
@Composable
private fun PlanPanel(plan: Plan.Status?, days: List<Plan.Day>, usageAccess: Boolean, onEdit: () -> Unit, onAllow: () -> Unit, locked: Boolean) {
  Panel {
    Row(modifier = Modifier.fillMaxWidth(), verticalAlignment = Alignment.CenterVertically) {
      PanelTitle("Aylık paket", Modifier.weight(1f))
      TextButton(onClick = onEdit, enabled = !locked) { Text(if (plan == null) "Paketi gir" else "Değiştir", style = MaterialTheme.typography.labelLarge) }
    }
    if (plan == null) {
      Caption("Paketinin kaç GB olduğunu ve ayın kaçında yenilendiğini gir. Kalan veriyi ve paketin yetmesi için günde ne kadar kullanabileceğini burada görürsün.")
    } else {
      val limit = plan.limitMb * 1024 * 1024
      val share = (plan.used.toFloat() / limit).coerceIn(0f, 1f)
      Spacer(Modifier.height(4.dp))
      Text("${humanBytes(plan.used)} / ${limitLabel(plan.limitMb)}", style = MaterialTheme.typography.headlineSmall, color = MaterialTheme.colorScheme.onSurface)
      Spacer(Modifier.height(8.dp))
      LinearProgressIndicator(
        progress = { share },
        modifier = Modifier.fillMaxWidth(),
        color = if (share >= 0.9f) HotspotTheme.status.warn else MaterialTheme.colorScheme.primary,
      )
      Spacer(Modifier.height(6.dp))
      Caption(
        if (plan.used >= limit) "Paket bitti · ${plan.daysLeft} gün sonra yenilenir"
        else "${humanBytes(limit - plan.used)} kaldı · yenilenmesine ${plan.daysLeft} gün var · günde en fazla ${humanBytes(plan.perDay)}"
      )
    }
    if (days.any { it.shared > 0 || (it.phone ?: 0) > 0 }) {
      Spacer(Modifier.height(14.dp))
      UsageChart(days)
    }
    Spacer(Modifier.height(8.dp))
    if (usageAccess) {
      Caption("Telefonun bütün mobil veri kullanımı sayılıyor. Grafikteki koyu kısım paylaşılan veri.")
    } else {
      Row(verticalAlignment = Alignment.CenterVertically) {
        Caption("Şu an yalnızca paylaşılan veri sayılıyor. Telefonun kendi kullanımı da sayılsın istersen kullanım erişimi izni ver.", Modifier.weight(1f))
        TextButton(onClick = onAllow, enabled = !locked) { Text("İzin ver", style = MaterialTheme.typography.labelLarge) }
      }
    }
  }
}

/** The last days as bars, today last: the whole phone faint (when it can be read), sharing's part strong. */
@Composable
private fun UsageChart(days: List<Plan.Day>) {
  val strong = MaterialTheme.colorScheme.primary
  val faint = MaterialTheme.colorScheme.primary.copy(alpha = 0.3f)
  val track = MaterialTheme.colorScheme.outlineVariant
  val top = days.maxOf { maxOf(it.shared, it.phone ?: 0) }.coerceAtLeast(1)
  Column {
    Caption("En yüksek gün: ${humanBytes(top)}")
    Spacer(Modifier.height(6.dp))
    Canvas(modifier = Modifier.fillMaxWidth().height(72.dp)) {
      val slot = size.width / days.size
      val bar = slot * 0.62f
      drawLine(track, Offset(0f, size.height), Offset(size.width, size.height), strokeWidth = 1.dp.toPx())
      days.forEachIndexed { i, d ->
        val x = i * slot + (slot - bar) / 2
        val whole = maxOf(d.phone ?: 0, d.shared)
        if (whole > 0) {
          val h = (whole.toFloat() / top * size.height).coerceAtLeast(2.dp.toPx())
          drawRoundRect(faint, Offset(x, size.height - h), Size(bar, h), CornerRadius(2.dp.toPx()))
        }
        if (d.shared > 0) {
          val h = (d.shared.toFloat() / top * size.height).coerceAtLeast(2.dp.toPx())
          drawRoundRect(strong, Offset(x, size.height - h), Size(bar, h), CornerRadius(2.dp.toPx()))
        }
      }
    }
    Spacer(Modifier.height(4.dp))
    Row(modifier = Modifier.fillMaxWidth()) {
      Caption("${days.first().date.dayOfMonth}.${days.first().date.monthValue}", Modifier.weight(1f))
      Caption("Bugün")
    }
  }
}

/** The speed test of the phone's internet: the last result and the button. */
@Composable
private fun SpeedPanel(speed: SpeedTest.State, onStart: () -> Unit) {
  Panel {
    Row(modifier = Modifier.fillMaxWidth(), verticalAlignment = Alignment.CenterVertically) {
      PanelTitle("Hız testi", Modifier.weight(1f))
      TextButton(onClick = onStart, enabled = !speed.running) {
        Text(if (speed.running) "Ölçülüyor…" else "Ölç", style = MaterialTheme.typography.labelLarge)
      }
    }
    if (speed.running) {
      Spacer(Modifier.height(6.dp))
      LinearProgressIndicator(modifier = Modifier.fillMaxWidth())
      speed.stage?.let {
        Spacer(Modifier.height(6.dp))
        Caption(it)
      }
    }
    speed.last?.let { r ->
      Spacer(Modifier.height(8.dp))
      Row(modifier = Modifier.fillMaxWidth()) {
        Metric(AppIcons.ArrowDown, "İndirme", bitRate(r.downBps), Modifier.weight(1f))
        Metric(AppIcons.ArrowUp, "Yükleme", bitRate(r.upBps), Modifier.weight(1f))
        Metric(AppIcons.Link, "Ping", "${r.pingMs} ms", Modifier.weight(0.7f))
      }
      if (r.wifiDownBps > 0) {
        Spacer(Modifier.height(8.dp))
        Caption("Telefonla aradaki Wi-Fi: ↓ ${bitRate(r.wifiDownBps)} · ↑ ${bitRate(r.wifiUpBps)}")
        bottleneck(r)?.let { Caption(it) }
      }
      Spacer(Modifier.height(6.dp))
      Caption("${ago(r.at)} ölçüldü · ${humanBytes(r.bytes)} mobil veri harcandı")
    }
    speed.error?.let {
      Spacer(Modifier.height(6.dp))
      Caption(it)
    }
    if (speed.last == null && speed.error == null && !speed.running) {
      Caption("Telefonun mobil internetinin hızını ölçer; en fazla yaklaşık 55 MB veri harcar.")
    }
  }
}

/** "3 dk önce" for a time in Unix seconds. */
private fun ago(unixSeconds: Long): String {
  val minutes = (System.currentTimeMillis() / 1000 - unixSeconds).coerceAtLeast(0) / 60
  return when {
    minutes < 1 -> "Az önce"
    minutes < 60 -> "$minutes dk önce"
    minutes < 24 * 60 -> "${minutes / 60} saat önce"
    else -> "${minutes / (24 * 60)} gün önce"
  }
}

@Composable
private fun SwitchRow(title: String, caption: String, checked: Boolean, onChange: (Boolean) -> Unit, enabled: Boolean = true) {
  Row(verticalAlignment = Alignment.CenterVertically) {
    Column(modifier = Modifier.weight(1f)) {
      Text(title, style = MaterialTheme.typography.bodyLarge, color = MaterialTheme.colorScheme.onSurface)
      Caption(caption)
    }
    Spacer(Modifier.width(12.dp))
    Switch(checked = checked, onCheckedChange = onChange, enabled = enabled)
  }
}

/** A row that opens a choice: its name on the left, what is chosen on the right. */
@Composable
private fun ChoiceRow(title: String, value: String, onClick: () -> Unit, enabled: Boolean = true) {
  Row(
    modifier = Modifier.fillMaxWidth().heightIn(min = 40.dp).alpha(if (enabled) 1f else 0.38f)
      .clickable(enabled = enabled, role = Role.Button, onClick = onClick),
    verticalAlignment = Alignment.CenterVertically,
  ) {
    Text(title, modifier = Modifier.weight(1f), style = MaterialTheme.typography.bodyLarge, color = MaterialTheme.colorScheme.onSurface)
    Spacer(Modifier.width(12.dp))
    Text(value, style = MaterialTheme.typography.labelLarge, color = MaterialTheme.colorScheme.primary)
  }
}

@Composable
private fun RowDivider() = HorizontalDivider(modifier = Modifier.padding(vertical = 14.dp), color = MaterialTheme.colorScheme.outlineVariant)

/** A group of settings: its name above, its rows in one panel. */
@Composable
private fun SettingsGroup(title: String, content: @Composable ColumnScope.() -> Unit) {
  Text(
    title,
    modifier = Modifier.padding(start = 4.dp, bottom = 8.dp),
    style = MaterialTheme.typography.labelLarge,
    color = MaterialTheme.colorScheme.primary,
  )
  Panel(content = content)
  Spacer(Modifier.height(20.dp))
}

/**
 * Everything there is to set, in groups: what happens to the shared traffic, the phone itself, the Wi-Fi network,
 * the Quick Settings tile, and the address the QR codes carry (folded away: most people never need it).
 */
@Composable
private fun SettingsContent(
  blockAds: Boolean,
  saveData: Boolean,
  priority: Boolean,
  splitTls: Boolean,
  blocked: Long?,
  splitCount: Long?,
  onBlockAds: (Boolean) -> Unit,
  onSaveData: (Boolean) -> Unit,
  onPriority: (Boolean) -> Unit,
  onSplit: (Boolean) -> Unit,
  guard: Boolean,
  onGuard: (Boolean) -> Unit,
  idleMin: Long,
  onIdle: () -> Unit,
  ownNetwork: Boolean,
  ownNetworkEditable: Boolean,
  onOwnNetwork: (Boolean) -> Unit,
  band5: Boolean,
  onBand5: (Boolean) -> Unit,
  tileAdded: Boolean,
  onAddTile: (() -> Unit)?,
  onRemoveTile: () -> Unit,
  addressesOpen: Boolean,
  onToggleAddresses: () -> Unit,
  addresses: List<LocalAddress>,
  selected: LocalAddress?,
  port: Int,
  onSelect: (String) -> Unit,
  locked: Boolean,
) {
  // A device that joined another phone does not share: what only sharing uses is locked until it stops.
  if (locked) {
    Banner(Tone.Info, AppIcons.Link, "Bu cihaz şu an başka bir telefona bağlı. Paylaşım ayarları, bağlantıyı durdurunca açılır.")
    Spacer(Modifier.height(16.dp))
  }
  SettingsGroup("Koruma ve tasarruf") {
    SwitchRow(
      "Reklam ve izleyici engelleme",
      "Bağlı bütün cihazlarda reklamlar ve izleyiciler engellenir." + (blocked?.takeIf { blockAds && it > 0 }?.let { " Bu paylaşımda $it istek engellendi." } ?: ""),
      blockAds, onBlockAds, enabled = !locked,
    )
    RowDivider()
    SwitchRow("Veri tasarrufu", "Paylaşım açıkken sistem ve oyun güncellemeleri indirilmez.", saveData, onSaveData, enabled = !locked)
    RowDivider()
    SwitchRow("Akıllı öncelik", "Görüşme ya da oyun sırasında büyük indirmeler yavaşlatılır.", priority, onPriority, enabled = !locked)
    RowDivider()
    SwitchRow(
      "AleDPI modu",
      if (splitTls && splitCount != null) "Açık. Bu paylaşımda $splitCount bağlantıda kullanıldı." else "Açılmayan siteleri açmayı dener. Gerekmiyorsa kapalı tut.",
      splitTls, onSplit, enabled = !locked,
    )
  }

  SettingsGroup("Telefon") {
    SwitchRow("Pil ve ısı koruması", "Telefon ısınınca paylaşım yavaşlar. Pil %${Guard.BATTERY_WARN}'e düşünce uyarır, %${Guard.BATTERY_STOP}'e düşünce paylaşımı durdurur.", guard, onGuard, enabled = !locked)
    RowDivider()
    ChoiceRow("Kimse bağlı değilken kapat", if (idleMin > 0) "${idleLabel(idleMin)} sonra" else "Kapalı", onIdle, enabled = !locked)
  }

  SettingsGroup("Wi-Fi ağı") {
    SwitchRow(
      "Wi-Fi ağını uygulama açsın",
      if (ownNetworkEditable) "Kapatırsan telefonun kendi hotspot'unu ya da başka bir ağı sen açarsın." else "Paylaşım açıkken değiştirilemez.",
      ownNetwork, onOwnNetwork, enabled = ownNetworkEditable && !locked,
    )
    if (ownNetwork) {
      RowDivider()
      Text("Bant", style = MaterialTheme.typography.bodyLarge, color = MaterialTheme.colorScheme.onSurface)
      Spacer(Modifier.height(8.dp))
      SegmentedSwitch(options = listOf(false to "2,4 GHz", true to "5 GHz"), selected = band5, onSelect = { if (it != band5) onBand5(it) }, enabled = !locked)
      Spacer(Modifier.height(6.dp))
      Caption(if (band5) "Daha hızlıdır. Bilgisayarın ağı göremezse 2,4 GHz'e dön." else "Her cihaz görür ama 5 GHz kadar hızlı değildir.")
    }
  }

  if (onAddTile != null) {
    SettingsGroup("Kısayol") {
      TileRow(
        icon = AppIcons.Toggle,
        title = "Paylaşım düğmesi",
        addedCaption = "Hızlı ayarlara eklendi; paylaşımı tek dokunuşla açıp kapatır.",
        freeCaption = "Paylaşımı bildirim panelinden tek dokunuşla açıp kapatmak için ekle.",
        added = tileAdded,
        onAdd = onAddTile,
        onRemove = onRemoveTile,
        enabled = !locked,
      )
    }
  }

  SettingsGroup("Güncelleme") {
    val context = LocalContext.current
    val online by OnlineUpdate.state.collectAsStateWithLifecycle()
    var auto by remember { mutableStateOf(OnlineUpdate.enabled(context)) }
    SwitchRow(
      "Yeni sürümleri denetle",
      "Birkaç saatte bir GitHub'a bakar ve yeni sürüm yayımlanınca haber verir. İndirme ancak sen isteyince başlar.",
      auto,
      { auto = it; OnlineUpdate.setEnabled(context, it) },
    )
    RowDivider()
    ChoiceRow(
      "Şimdi denetle",
      when {
        online.checking -> "Bakılıyor…"
        online.offer != null -> "${online.offer?.name} yayımlandı"
        else -> ""
      },
      onClick = {
        OnlineUpdate.check(context, force = true) { answered ->
          Handler(Looper.getMainLooper()).post {
            val offer = OnlineUpdate.state.value.offer
            val text = when {
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

  SettingsGroup("Gelişmiş") {
    val turn by animateFloatAsState(if (addressesOpen) 180f else 0f, label = "chevron")
    Row(
      modifier = Modifier.fillMaxWidth().heightIn(min = 40.dp).alpha(if (locked) 0.38f else 1f)
        .clickable(enabled = !locked, role = Role.Button, onClick = onToggleAddresses),
      verticalAlignment = Alignment.CenterVertically,
    ) {
      Column(modifier = Modifier.weight(1f)) {
        Text("Bağlantı adresi", style = MaterialTheme.typography.bodyLarge, color = MaterialTheme.colorScheme.onSurface)
        Caption(selected?.let { "${it.ip}:$port" } ?: "Henüz adres yok")
      }
      Icon(
        AppIcons.ChevronDown,
        contentDescription = if (addressesOpen) "Daralt" else "Genişlet",
        modifier = Modifier.size(20.dp).rotate(turn),
        tint = MaterialTheme.colorScheme.onSurfaceVariant,
      )
    }
    AnimatedVisibility(visible = addressesOpen && !locked, enter = fadeIn() + expandVertically(), exit = fadeOut() + shrinkVertically()) {
      Column {
        Spacer(Modifier.height(8.dp))
        if (addresses.isEmpty()) {
          Caption("Telefonun paylaşım ağında henüz bir adresi yok. Önce paylaşımı başlat ya da telefonun kendi hotspot'unu aç.")
        }
        addresses.forEach { a ->
          val isSelected = a.ip == selected?.ip
          Row(
            modifier = Modifier.fillMaxWidth().selectable(selected = isSelected, onClick = { onSelect(a.ip) }, role = Role.RadioButton).padding(vertical = 6.dp),
            verticalAlignment = Alignment.CenterVertically,
          ) {
            RadioButton(selected = isSelected, onClick = null)
            Spacer(Modifier.width(12.dp))
            Column {
              Text("${a.ip}:$port", style = MaterialTheme.typography.bodyLarge, fontFamily = FontFamily.Monospace, color = MaterialTheme.colorScheme.onSurface)
              Caption(a.ifName)
            }
          }
        }
        Spacer(Modifier.height(6.dp))
        Caption("Telefon veya tablet eklerken seçili adres QR koda yazılır.")
      }
    }
  }
}

/** The line under a device's name: why it is paused, or what it used today and the rules it has. */
private fun deviceNote(context: Context, name: String, used: Long): String {
  val limit = Controls.deviceLimitMb(context, name)
  val quiet = Controls.quietHours(context, name)
  return when (Controls.pause(context, name)) {
    Controls.Pause.Manual -> "Duraklatıldı"
    Controls.Pause.Limit -> "Günlük sınır doldu · gece yarısı devam eder"
    Controls.Pause.Quiet -> (quiet?.let { "İnternet kapalı · ${QuietHours.clockAt(it.to)} açılır" } ?: "İnternet kapalı")
    Controls.Pause.None -> listOfNotNull(
      Guests.note(context, name),
      used.takeIf { it > 0 }?.let { "Bugün ${humanBytes(it)}" },
      limit.takeIf { it > 0 }?.let { "sınır ${limitLabel(it)}" },
      Controls.deviceSpeedKbps(context, name).takeIf { it > 0 }?.let { bitRate(it * 1000) },
      quiet?.let { "${it.label()} arası kapalı" },
    ).joinToString(" · ")
  }
}

/** "30 dk", "1 saat", "2 saat". */
internal fun idleLabel(minutes: Long): String = if (minutes >= 60 && minutes % 60 == 0L) "${minutes / 60} saat" else "$minutes dk"

@Composable
private fun TrafficPanel(t: TrafficStats) {
  Panel {
    PanelTitle("Bu oturum")
    Spacer(Modifier.height(12.dp))
    Row(modifier = Modifier.fillMaxWidth()) {
      Metric(AppIcons.ArrowUp, "Yükleme", humanBytes(t.bytesUp), Modifier.weight(1f))
      Metric(AppIcons.ArrowDown, "İndirme", humanBytes(t.bytesDown), Modifier.weight(1f))
      Metric(AppIcons.Link, "Bağlantı", (t.tcpActive + t.udpActive).toString(), Modifier.weight(0.7f))
    }
    val dns =
      when {
        t.dnsPlain > 0 -> "${t.dnsPlain} DNS sorgusu şifresiz gitti (şifreli sunuculara ulaşılamadı)"
        t.dnsEncrypted > 0 -> "DNS sorguları şifreli"
        else -> null
      }
    val notes = listOfNotNull(dns, t.dialErrors.takeIf { it > 0 }?.let { "Ulaşılamayan adres: $it" })
    if (notes.isNotEmpty()) {
      Spacer(Modifier.height(12.dp))
      Caption(notes.joinToString("  ·  "))
    }
  }
}

@Composable
private fun DevicesPanel(
  peers: List<String>,
  notes: Map<String, String>,
  sharing: Boolean,
  onAdd: () -> Unit,
  onRemove: (String) -> Unit,
  onOpen: (String) -> Unit,
  locked: Boolean,
) {
  Panel {
    Row(modifier = Modifier.fillMaxWidth(), verticalAlignment = Alignment.CenterVertically) {
      PanelTitle("Cihazlar", Modifier.weight(1f))
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
    Spacer(Modifier.height(8.dp))
    if (locked) {
      Caption("Bu cihaz başka bir telefona bağlıyken cihaz eklenemez ve ayarlar değiştirilemez.", Modifier.padding(bottom = 8.dp))
    } else if (peers.isEmpty()) {
      Caption(
        if (sharing) "Henüz cihaz yok. Bilgisayar eklemek için bilgisayarda AleShare.exe'yi aç, sonra burada Ekle → Bilgisayar'a dokunup ekrandaki QR kodu tara."
        else "Henüz cihaz yok. Bilgisayar eklemek için önce paylaşımı başlat.",
        Modifier.padding(vertical = 8.dp),
      )
    }
    peers.forEachIndexed { i, name ->
      if (i > 0) HorizontalDivider(color = MaterialTheme.colorScheme.outlineVariant)
      Row(
        modifier = Modifier.fillMaxWidth().clickable(enabled = !locked, role = Role.Button, onClick = { onOpen(name) }).padding(vertical = 6.dp),
        verticalAlignment = Alignment.CenterVertically,
      ) {
        Avatar(name)
        Spacer(Modifier.width(14.dp))
        Column(modifier = Modifier.weight(1f)) {
          Text(
            name,
            style = MaterialTheme.typography.bodyLarge,
            color = MaterialTheme.colorScheme.onSurface,
            maxLines = 1,
            overflow = TextOverflow.Ellipsis,
          )
          notes[name]?.takeIf { it.isNotEmpty() }?.let { Caption(it) }
        }
        IconAction(AppIcons.Trash, "$name cihazını kaldır", onClick = { onRemove(name) }, enabled = !locked)
      }
    }
  }
}

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
    Caption(listOfNotNull(version?.let { "Sürüm $it" }, "Yapımcı: $MAKER").joinToString("  ·  "))
  }
}

/** Today's mobile data: in all, against the daily limit when there is one, and the button that sets the limit. */
@Composable
private fun UsagePanel(usage: DataUsage.Today, limitMb: Long, onLimit: () -> Unit, locked: Boolean) {
  val limit = limitMb * 1024 * 1024
  Panel {
    Row(modifier = Modifier.fillMaxWidth(), verticalAlignment = Alignment.CenterVertically) {
      Column(modifier = Modifier.weight(1f)) {
        Caption("Bugün paylaşılan veri")
        Text(humanBytes(usage.total), style = MaterialTheme.typography.headlineSmall, color = MaterialTheme.colorScheme.onSurface)
      }
      TextButton(onClick = onLimit, enabled = !locked) {
        Text(if (limitMb > 0) "Sınır: ${limitLabel(limitMb)}" else "Sınır koy", style = MaterialTheme.typography.labelLarge)
      }
    }
    if (limit > 0) {
      val share = (usage.total.toFloat() / limit).coerceIn(0f, 1f)
      val over = usage.total >= limit
      Spacer(Modifier.height(10.dp))
      LinearProgressIndicator(
        progress = { share },
        modifier = Modifier.fillMaxWidth(),
        color = if (over || share >= 0.9f) HotspotTheme.status.warn else MaterialTheme.colorScheme.primary,
      )
      Spacer(Modifier.height(6.dp))
      Caption(
        if (over) "Bugünkü sınır doldu ve paylaşım durduruldu. Yeniden başlatırsan bugün bir daha durmaz."
        else "Sınırın ${Tr.share((share * 100).toInt())} kullanıldı · gece yarısı sıfırlanır"
      )
    } else {
      Spacer(Modifier.height(4.dp))
      Caption("Sayaç gece yarısı sıfırlanır. Her cihazın kullanımını Paylaş sekmesindeki cihaz listesinde görürsün.")
    }
  }
}

private fun peerNames(core: Core): List<String> = core.peerNames().split("\n").filter { it.isNotBlank() }

private fun isIgnoringBattery(context: Context): Boolean =
  context.getSystemService(PowerManager::class.java).isIgnoringBatteryOptimizations(context.packageName)


private fun setOwnNetworkPref(context: Context, value: Boolean) {
  context.getSharedPreferences("settings", Context.MODE_PRIVATE).edit().putBoolean("own_network", value).apply()
}

private fun requestIgnoreBattery(context: Context) {
  context.startActivity(Intent(Settings.ACTION_REQUEST_IGNORE_BATTERY_OPTIMIZATIONS, Uri.parse("package:${context.packageName}")))
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
    context.startActivity(Intent(Settings.ACTION_MANAGE_UNKNOWN_APP_SOURCES, Uri.parse("package:${context.packageName}")))
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
