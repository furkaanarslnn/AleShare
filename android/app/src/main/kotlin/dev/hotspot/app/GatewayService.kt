package dev.hotspot.app

import android.app.Notification
import android.app.NotificationChannel
import android.app.NotificationManager
import android.app.PendingIntent
import android.app.Service
import android.content.BroadcastReceiver
import android.content.Context
import android.content.Intent
import android.content.IntentFilter
import android.content.pm.ServiceInfo
import android.net.wifi.WifiManager
import android.net.wifi.p2p.WifiP2pGroup
import android.net.wifi.p2p.WifiP2pManager
import android.os.Build
import android.os.Handler
import android.os.IBinder
import android.os.Looper
import android.os.PowerManager
import android.os.SystemClock
import androidx.core.app.NotificationCompat
import androidx.core.app.ServiceCompat
import androidx.core.content.ContextCompat
import dev.hotspot.wgcore.Core
import kotlin.concurrent.thread

/** Runs the Go core as a foreground service so Android keeps the gateway alive. */
class GatewayService : Service() {
  private var cellular: CellularNetwork? = null
  private var wakeLock: PowerManager.WakeLock? = null
  private var group: WifiDirectGroup? = null
  private var credentials: WifiCredentials? = null
  private var watcher: BroadcastReceiver? = null
  private val handler = Handler(Looper.getMainLooper())
  private var reopenAttempts = 0

  /** The network is being replaced: the group is closed on purpose, so the watcher must not reopen it. */
  private var switching = false
  private var changeDeadline = 0L
  private val changeWatcher = Runnable { watchChange() }

  override fun onBind(intent: Intent?): IBinder? = null

  override fun onStartCommand(intent: Intent?, flags: Int, startId: Int): Int {
    when (intent?.action) {
      ACTION_STOP -> {
        shutDown()
        stopSelf()
        return START_NOT_STICKY
      }
      ACTION_CHANGE_WIFI, ACTION_SWITCH_NOW, ACTION_CANCEL_CHANGE, ACTION_CHANGE_BAND -> {
        if (!GatewayState.state.value.running) {
          WifiNetwork.clearNext(this)
          stopSelf()
          return START_NOT_STICKY
        }
        when (intent.action) {
          ACTION_CHANGE_WIFI -> beginWifiChange()
          ACTION_SWITCH_NOW -> switchWifi()
          ACTION_CHANGE_BAND -> changeBand()
          else -> cancelWifiChange()
        }
        return START_STICKY
      }
      ACTION_TOGGLE -> {
        // The widget's tap. Android started this as a foreground service and wants it to be one, if only for a
        // moment, even when the tap stops sharing.
        val s = GatewayState.state.value
        val sharing = s.running || s.starting
        if (sharing || ClientState.state.value.phase != ClientPhase.Idle) {
          runCatching {
            ServiceCompat.startForeground(this, NOTIFICATION_ID, notification(shownNote ?: LiveNote("Durduruluyor", "")), ServiceInfo.FOREGROUND_SERVICE_TYPE_SPECIAL_USE)
          }
          shutDown()
          if (!sharing) {
            GatewayState.update { it.copy(error = "Bu cihaz başka bir telefona bağlıyken paylaşım yapamaz. Önce Bağlan sekmesinden bağlantıyı durdur.") }
            ShareControls.refresh(this)
          }
          stopSelf()
          return START_NOT_STICKY
        }
      }
    }
    // No intent: Android brings the service back after it had to end the app's process. Share as before; the
    // computers and phones that use it then come back by themselves. The widget's tap shares the same way.
    val port = intent?.getIntExtra(EXTRA_PORT, DEFAULT_PORT) ?: DEFAULT_PORT
    val ownNetwork = if (intent?.hasExtra(EXTRA_OWN_NETWORK) == true) intent.getBooleanExtra(EXTRA_OWN_NETWORK, false) else ownNetworkPreference(this)
    if (!startGateway(port, ownNetwork)) {
      stopSelf()
      return START_NOT_STICKY
    }
    return START_STICKY
  }

  /** Returns false when Android does not let the service run in the foreground now. */
  private fun startGateway(port: Int, ownNetwork: Boolean): Boolean {
    // Must come first: Android gives a few seconds to show the notification.
    try {
      ServiceCompat.startForeground(
        this, NOTIFICATION_ID, notification(shownNote ?: LiveNote("Hazırlanıyor", "Mobil veri ve Wi-Fi hazırlanıyor.")),
        ServiceInfo.FOREGROUND_SERVICE_TYPE_SPECIAL_USE,
      )
    } catch (e: Exception) {
      // A restart in the background may be refused (ForegroundServiceStartNotAllowedException).
      GatewayState.update { it.copy(error = e.message ?: e.toString()) }
      ShareControls.refresh(this)
      return false
    }
    if (GatewayState.state.value.running) return true

    GatewayState.update { it.copy(starting = true, error = null) }
    ShareControls.refresh(this)
    // A change that was under way when sharing ended (or the app was killed) is dropped; devices that learned it
    // keep looking for both networks and forget the new one when the phone no longer tells it.
    WifiNetwork.clearNext(this)
    if (!ownNetwork) {
      begin(port, null)
      return true
    }
    val wifi = WifiNetwork.credentials(this)
    credentials = wifi
    val g = WifiDirectGroup(this)
    group = g
    g.create(wifi) { error ->
      if (error != null) {
        fail(error)
      } else {
        begin(port, wifi.ssid)
        showBand(g)
      }
    }
    return true
  }

  private fun showBand(g: WifiDirectGroup) {
    g.frequency { mhz -> GatewayState.update { it.copy(band = bandLabel(mhz)) } }
  }

  /** The user picked the other band: the network closes and opens again on it, with the same name and password. */
  private fun changeBand() {
    val wifi = credentials
    val g = group
    if (wifi == null || g == null || switching) return
    switching = true
    g.replace(wifi, wifi) { error ->
      switching = false
      reopenAttempts = 0
      if (error != null) GatewayState.update { it.copy(error = "Wi-Fi ağı yeni bantta açılamadı. ($error)") }
      showBand(g)
    }
  }

  /** The network is there (or not needed): bring the core up. */
  private fun begin(port: Int, network: String?) {
    val net = CellularNetwork(this).also { it.start() }
    cellular = net
    try {
      val core = CoreHolder.get(this)
      core.setSplitTLS(splitPreference(this)) // before the first connection, not after it
      // Guests whose time ran out while sharing was off do not get in again.
      val expired = Guests.over(this)
      expired.forEach { core.removePeer(it); Guests.forget(this, it) }
      CoreHolder.save(this, core)
      // The store's token stays from the last run (a computer's window keeps reading the phone), unless a device left.
      if (expired.isEmpty()) core.setUpdateToken(Updates.token(this)) else Updates.renewToken(this, core)
      Controls.applyAll(this, core, core.peerNames().lines().filter { it.isNotBlank() })
      Guard.reset()
      SpeedTest.load(this)
      lastBusyAt = SystemClock.elapsedRealtime()
      planAt = 0L
      // The releases paired devices take from here, or bring here (see Updates): the newest APK this phone has,
      // and the Windows program that came inside the app.
      runCatching {
        core.setUpdateStore(Updates.storeDir(this).path)
        Updates.bundledProgram(this)?.let { core.setPCProgram(it.path) }
        offerApp(core, Updates.bestApk(this))
      }
      DataUsage.started()
      core.start(port.toLong(), CellularProtector(net))
      // Paired devices may ask for the network's name and password: that is how they follow a change.
      credentials?.let { core.setWifi(it.ssid, it.passphrase, "", "") } ?: core.setWifi("", "", "", "")
      // Held for as long as the gateway runs; narrowing this to "clients are active" needs measurements first.
      wakeLock = getSystemService(PowerManager::class.java)
        .newWakeLock(PowerManager.PARTIAL_WAKE_LOCK, "hotspot:gateway")
        .apply {
          setReferenceCounted(false)
          acquire()
        }
      GatewayState.update { it.copy(running = true, starting = false, port = port, network = network) }
      sharedSince = System.currentTimeMillis()
      setSharingOn(this, true)
      if (network != null) {
        watchGroup()
        watchWifi()
      }
      ShareControls.refresh(this, 0)
      handler.post(notifier)
    } catch (e: Exception) {
      fail(e.message ?: e.toString())
    }
  }

  private fun fail(message: String) {
    shutDown()
    GatewayState.update { it.copy(error = message) }
    ShareControls.refresh(this)
    stopSelf()
  }

  /**
   * Some phones close a Wi-Fi Direct group when nobody has been in it for a while.
   * If ours disappears while sharing, open it again (a few times, then give up).
   */
  private fun watchGroup() {
    val receiver = object : BroadcastReceiver() {
      override fun onReceive(context: Context, intent: Intent) {
        val g = if (Build.VERSION.SDK_INT >= 33) {
          intent.getParcelableExtra(WifiP2pManager.EXTRA_WIFI_P2P_GROUP, WifiP2pGroup::class.java)
        } else {
          @Suppress("DEPRECATION") intent.getParcelableExtra(WifiP2pManager.EXTRA_WIFI_P2P_GROUP)
        }
        // A device joined or left the network: say so now, not at the next look.
        groupClients = g?.takeIf { it.networkName == credentials?.ssid }?.clientList?.size
        refreshNotification()
        handler.postDelayed({ credentials?.takeIf { !switching }?.let { checkGroup(it) } }, 2000)
      }
    }
    watcher = receiver
    ContextCompat.registerReceiver(
      this, receiver, IntentFilter(WifiP2pManager.WIFI_P2P_CONNECTION_CHANGED_ACTION), ContextCompat.RECEIVER_NOT_EXPORTED
    )
  }

  /**
   * The app's own network is Wi-Fi Direct, which goes when the user turns Wi-Fi off (or airplane mode on): then
   * sharing ends too, with the notification, instead of looking alive with nobody able to join.
   */
  private fun watchWifi() {
    val receiver = object : BroadcastReceiver() {
      override fun onReceive(context: Context, intent: Intent) {
        if (wifiGoingOff(intent.getIntExtra(WifiManager.EXTRA_WIFI_STATE, WifiManager.WIFI_STATE_UNKNOWN))) wifiTurnedOff()
      }
    }
    wifiWatcher = receiver
    // The broadcast is sticky: registering hands over the state now, which is "on" since the group just opened.
    ContextCompat.registerReceiver(this, receiver, IntentFilter(WifiManager.WIFI_STATE_CHANGED_ACTION), ContextCompat.RECEIVER_NOT_EXPORTED)
  }

  private var wifiWatcher: BroadcastReceiver? = null

  private fun wifiTurnedOff() {
    val s = GatewayState.state.value
    if (!s.running && !s.starting) return
    shutDown()
    GatewayState.update { it.copy(error = WIFI_OFF_MESSAGE) }
    ShareControls.refresh(this)
    // The sharing notification goes with the service; this one says why, and stays until it is read.
    val open = PendingIntent.getActivity(this, 0, Intent(this, MainActivity::class.java), PendingIntent.FLAG_IMMUTABLE)
    val n = NotificationCompat.Builder(this, CHANNEL_ID)
      .setSmallIcon(R.drawable.ic_stat_hotspot)
      .setColor(ContextCompat.getColor(this, R.color.accent))
      .setContentTitle("Paylaşım durdu")
      .setContentText("Wi-Fi kapatıldı. Yeniden paylaşmak için Wi-Fi'yi aç ve paylaşımı başlat.")
      .setContentIntent(open)
      .setAutoCancel(true)
      .setSilent(true)
      .setCategory(NotificationCompat.CATEGORY_STATUS)
      .build()
    runCatching { getSystemService(NotificationManager::class.java).notify(STOPPED_NOTIFICATION_ID, n) }
    stopSelf()
  }

  private fun checkGroup(wifi: WifiCredentials) {
    val g = group ?: return
    if (!GatewayState.state.value.running || switching || wifi != credentials) return
    g.isOpen(wifi) { open ->
      if (open) {
        reopenAttempts = 0
      } else if (reopenAttempts < MAX_REOPEN) {
        reopenAttempts++
        g.create(wifi) { error -> if (error != null) GatewayState.update { it.copy(error = error) } }
      }
    }
  }

  /**
   * The user picked a new name or password ([WifiNetwork.next]). The phone first tells it to the paired devices
   * that ask (computers ask once a minute, phones and tablets every half minute), so that they follow by
   * themselves; once every device connected now has learned it, or after [CHANGE_WAIT_MILLIS], the old network
   * closes and the new one opens.
   */
  private fun beginWifiChange() {
    val old = credentials
    val next = WifiNetwork.next(this)
    val core = CoreHolder.peek()
    if (old == null || next == null || core == null || next == old) {
      WifiNetwork.clearNext(this)
      return
    }
    core.setWifi(old.ssid, old.passphrase, next.ssid, next.passphrase)
    val waiting = connectedFollowers(this, core)
    changeDeadline = SystemClock.elapsedRealtime() + CHANGE_WAIT_MILLIS
    allLearnedAt = 0L
    GatewayState.update { it.copy(wifiChange = WifiChange(next.ssid, waiting, emptyList()), error = null) }
    handler.removeCallbacks(changeWatcher)
    handler.post(changeWatcher)
  }

  private var allLearnedAt = 0L

  private fun watchChange() {
    val change = GatewayState.state.value.wifiChange ?: return
    val core = CoreHolder.peek() ?: return
    val learned = core.wifiLearned().lines().filter { it.isNotBlank() }
    GatewayState.update { it.copy(wifiChange = change.copy(learned = learned)) }
    val now = SystemClock.elapsedRealtime()
    if (learned.containsAll(change.waitingFor) && allLearnedAt == 0L) allLearnedAt = now
    // A device that has just learned the new network still writes it down (a computer makes a Windows profile).
    if ((allLearnedAt != 0L && now - allLearnedAt >= SETTLE_MILLIS) || now >= changeDeadline) {
      switchWifi()
    } else {
      handler.postDelayed(changeWatcher, 1000)
    }
  }

  /** Closes the old network and opens the new one. If the new one cannot be opened, the old one comes back. */
  private fun switchWifi() {
    handler.removeCallbacks(changeWatcher)
    val old = credentials
    val g = group
    val next = WifiNetwork.commitNext(this)
    if (old == null || g == null || next == null) {
      GatewayState.update { it.copy(wifiChange = null) }
      return
    }
    val core = CoreHolder.peek()
    switching = true
    credentials = next
    core?.setWifi(next.ssid, next.passphrase, "", "")
    GatewayState.update { it.copy(wifiChange = null, network = next.ssid) }
    g.replace(old, next) { error ->
      switching = false
      if (error == null) {
        reopenAttempts = 0
        showBand(g)
        return@replace
      }
      WifiNetwork.restore(this, old)
      credentials = old
      core?.setWifi(old.ssid, old.passphrase, "", "")
      GatewayState.update { it.copy(network = old.ssid, error = "Yeni ağ açılamadı; eski ağa dönülüyor. ($error)") }
      g.create(old) { again -> if (again != null) fail(again) }
    }
  }

  private fun cancelWifiChange() {
    handler.removeCallbacks(changeWatcher)
    WifiNetwork.clearNext(this)
    credentials?.let { CoreHolder.peek()?.setWifi(it.ssid, it.passphrase, "", "") }
    GatewayState.update { it.copy(wifiChange = null) }
  }

  private fun shutDown() {
    handler.removeCallbacksAndMessages(null)
    shownNote = null
    sharedSince = 0L
    toldConnected = -1
    lastBytes = null
    groupClients = null
    WifiNetwork.clearNext(this)
    switching = false
    watcher?.let { runCatching { unregisterReceiver(it) } }
    watcher = null
    wifiWatcher?.let { runCatching { unregisterReceiver(it) } }
    wifiWatcher = null
    // The last reading before the core's counters go away with it.
    CoreHolder.peek()?.takeIf { GatewayState.state.value.running }?.let { DataUsage.record(this, it) }
    DataUsage.flush(this)
    CoreHolder.peek()?.setWifi("", "", "", "")
    CoreHolder.peek()?.stop()
    cellular?.stop()
    cellular = null
    credentials?.let { group?.remove(it) }
    group = null
    credentials = null
    reopenAttempts = 0
    wakeLock?.takeIf { it.isHeld }?.release()
    wakeLock = null
    GatewayState.update { it.copy(running = false, starting = false, cellular = false, network = null, wifiChange = null, band = null) }
    ShareControls.refresh(this, 0)
    // Every way sharing ends on purpose passes here. An update of the app ends the process without it, so the mark
    // stays and the new release starts sharing again (see BootReceiver).
    setSharingOn(this, false)
  }

  override fun onDestroy() {
    shutDown()
    super.onDestroy()
  }

  /** What the notification says; it is replaced only when that changes. */
  private var shownNote: LiveNote? = null

  /** When sharing began (epoch millis), for the notification's clock. */
  private var sharedSince = 0L

  /** The devices the widget and the tile were last told of. */
  private var toldConnected = -1

  /**
   * How many devices are in the Wi-Fi group, as the system last said (null: no group of ours). A device that leaves
   * the network is gone from it at once, long before its tunnel falls quiet.
   */
  private var groupClients: Int? = null

  private val notifier: Runnable = object : Runnable {
    override fun run() {
      refreshNotification()
      handler.postDelayed(this, NOTIFY_EVERY_MILLIS)
    }
  }

  /**
   * Keeps the notification saying how many devices use the internet now (those whose tunnel carried something a
   * moment ago, and on the app's own network no more than are in it), how fast it goes and what was shared. Looked
   * at every few seconds (a cheap reading of the counters; the phone is awake anyway while it shares), and at once
   * when the group changes.
   */
  private fun refreshNotification() {
    val core = CoreHolder.peek()
    if (!GatewayState.state.value.running || core == null) return
    // Changes a paired computer asked for.
    core.takeControls().lines().filter { it.isNotBlank() }.forEach { Controls.applyControl(this, core, it) }
    val active = core.activePeers(ACTIVE_SECONDS).lines().count { it.isNotBlank() }
    val connected = groupClients?.let { minOf(active, it) } ?: active
    val now = SystemClock.elapsedRealtime()
    if (connected > 0) lastBusyAt = now
    if (connected != toldConnected) {
      toldConnected = connected
      ShareControls.refresh(this, connected)
    }
    takeReceivedApp(core)
    OnlineUpdate.check(this) // is a newer release out on GitHub? Asked every few hours, a few kilobytes
    Guests.record(this, DataUsage.record(this, core))
    val stats = core.stats()
    val down = perSecond(stats.bytesDown, lastBytes?.first, now - lastBytesAt)
    val up = perSecond(stats.bytesUp, lastBytes?.second, now - lastBytesAt)
    lastBytes = stats.bytesDown to stats.bytesUp
    lastBytesAt = now
    show(shareNote(connected, down, up, DataUsage.today(this).total, stats.bytesUp + stats.bytesDown))
    endGuests(core)
    val devices = core.peerNames().lines().filter { it.isNotBlank() }
    for (name in Controls.enforceLimits(this, core, devices, DataUsage.today(this).devices)) alertDeviceLimit(name)
    if (!guard(core)) return
    if (now - planAt >= PLAN_EVERY_MILLIS) {
      planAt = now
      thread(name = "plan", isDaemon = true) { checkPlan() }
    }
    runCatching { core.setPhoneStatus(Controls.status(this, core, down, up, planStatus)) }
    when (DataUsage.limitAction(this)) {
      LimitAction.Warn -> alertUsage(stopped = false)
      LimitAction.Stop -> {
        alertUsage(stopped = true)
        shutDown()
        stopSelf()
        return
      }
      LimitAction.None -> {}
    }
    val idleMin = Controls.idleStopMin(this)
    if (idleOver(now, lastBusyAt, idleMin)) {
      notifyOnce(
        STOPPED_NOTIFICATION_ID, "Paylaşım kapandı",
        "$idleMin dakikadır hiçbir cihaz bağlı olmadığı için paylaşım kapatıldı. Süreyi Ayarlar → Telefon bölümünden değiştirebilirsin.",
      )
      shutDown()
      stopSelf()
    }
  }

  /** When a device last used the internet here (elapsed time), for stopping when nobody is connected. */
  private var lastBusyAt = 0L

  /** The plan as last read (off the main thread), told to paired computers. */
  @Volatile
  private var planStatus: Plan.Status? = null
  private var planAt = 0L

  private fun checkPlan() {
    val s = runCatching { Plan.status(this) }.getOrNull()
    planStatus = s
    val level = s?.let { Plan.alert(this, it) } ?: return
    notifyOnce(
      PLAN_NOTIFICATION_ID,
      if (level >= 100) "Aylık paket bitti" else "Aylık paketin %90'ı kullanıldı",
      "Bu dönem ${humanBytes(s.used)} / ${limitLabel(s.limitMb)} kullanıldı; paket ayın ${s.next.dayOfMonth}. günü yenilenir." +
        if (level >= 100) " Operatör hızı düşürebilir ya da ek ücret alabilir." else "",
    )
  }

  /** The guests whose time or data ran out lose their access: the device is removed. */
  private fun endGuests(core: Core) {
    val over = Guests.over(this)
    if (over.isEmpty()) return
    for (name in over) {
      core.removePeer(name)
      Guests.forget(this, name)
    }
    CoreHolder.save(this, core)
    Updates.renewToken(this, core) // the guest knew the store's token
    notifyOnce(GUEST_NOTIFICATION_ID, "Misafir erişimi bitti", "${over.joinToString()} artık bu telefonun internetini kullanamaz.")
  }

  /**
   * The battery and heat guard (see Guard): slows sharing down while the phone is hot, warns when the battery runs
   * low and stops sharing when it is nearly flat. Returns false when sharing was stopped.
   */
  private fun guard(core: Core): Boolean {
    val on = Controls.guard(this)
    val r = Guard.read(this)
    when (Guard.heat(r, on)) {
      Guard.Change.GotHot -> {
        core.setSpeedCap(Guard.HOT_CAP_KBPS)
        notifyOnce(GUARD_NOTIFICATION_ID, "Telefon ısındı", "Soğuyana kadar hız 10 Mbit/s ile sınırlandı. Soğuyunca kendiliğinden eski hızına döner.")
      }
      Guard.Change.CooledDown -> {
        core.setSpeedCap(0)
        getSystemService(NotificationManager::class.java).cancel(GUARD_NOTIFICATION_ID)
      }
      Guard.Change.None -> {}
    }
    when (Guard.battery(r, on)) {
      BatteryAction.Warn -> notifyOnce(
        GUARD_NOTIFICATION_ID, "Pil %${r.level}",
        "Paylaşım sürüyor ama pil %${Guard.BATTERY_STOP}'e düşerse duracak. Şarja takarsan durmaz.",
      )
      BatteryAction.Stop -> {
        notifyOnce(GUARD_NOTIFICATION_ID, "Pil azaldığı için paylaşım durdu", "Pil %${r.level}. Şarja takıp yeniden başlatabilirsin.")
        shutDown()
        stopSelf()
        return false
      }
      BatteryAction.None -> {}
    }
    return true
  }

  /** A notification of its own, on the channel of the phone's own warnings. */
  private fun notifyOnce(id: Int, title: String, text: String) {
    val nm = getSystemService(NotificationManager::class.java)
    nm.createNotificationChannel(
      NotificationChannel(GUARD_CHANNEL_ID, "Uyarılar", NotificationManager.IMPORTANCE_DEFAULT).apply {
        description = "Pil, ısı, aylık paket, misafirler ve kendiliğinden kapanan paylaşım için haber verir."
      }
    )
    val open = PendingIntent.getActivity(this, 0, Intent(this, MainActivity::class.java), PendingIntent.FLAG_IMMUTABLE)
    val n = NotificationCompat.Builder(this, GUARD_CHANNEL_ID)
      .setSmallIcon(R.drawable.ic_stat_hotspot)
      .setColor(ContextCompat.getColor(this, R.color.accent))
      .setContentTitle(title)
      .setContentText(text)
      .setStyle(NotificationCompat.BigTextStyle())
      .setContentIntent(open)
      .setAutoCancel(true)
      .setCategory(NotificationCompat.CATEGORY_STATUS)
      .build()
    runCatching { nm.notify(id, n) }
  }

  /** The gateway's byte counters at the last look, for the speed now. */
  /** The gateway's byte counters at the last look (down, up), for the speed now; null before the first. */
  private var lastBytes: Pair<Long, Long>? = null
  private var lastBytesAt = 0L

  private fun alertDeviceLimit(name: String) {
    val nm = getSystemService(NotificationManager::class.java)
    nm.createNotificationChannel(
      NotificationChannel(USAGE_CHANNEL_ID, "Veri sınırı", NotificationManager.IMPORTANCE_DEFAULT).apply {
        description = "Günlük veri sınırına yaklaşınca ve sınır dolunca haber verir."
      }
    )
    val open = PendingIntent.getActivity(this, 0, Intent(this, MainActivity::class.java), PendingIntent.FLAG_IMMUTABLE)
    val n = NotificationCompat.Builder(this, USAGE_CHANNEL_ID)
      .setSmallIcon(R.drawable.ic_stat_hotspot)
      .setColor(ContextCompat.getColor(this, R.color.accent))
      .setContentTitle("$name günlük sınırını doldurdu")
      .setContentText("$name gece yarısına kadar duraklatıldı. Cihazlar'dan devam ettirebilir ya da sınırı değiştirebilirsin.")
      .setStyle(NotificationCompat.BigTextStyle())
      .setContentIntent(open)
      .setAutoCancel(true)
      .build()
    runCatching { nm.notify(DEVICE_LIMIT_NOTIFICATION_BASE + (name.hashCode() and 0xff), n) }
  }

  /** Offers [apk] to the paired devices, and to this phone itself when it is newer than the installed app. */
  private fun offerApp(core: Core, apk: Updates.Apk) {
    core.setAppUpdate(apk.path, apk.code, apk.name)
    GatewayState.update { it.copy(pendingApp = apk.takeIf { a -> a.code > ownVersionCode(this) }) }
  }

  /**
   * A paired device brought a newer APK: if it is this app signed with this app's key, offer it to the other
   * devices and to this phone (a notification; Android asks before installing). Anything else is thrown away.
   */
  private fun takeReceivedApp(core: Core) {
    val received = core.receivedApp().takeIf { it.isNotEmpty() } ?: return
    val (path, code) = received.split('\t').let { it[0] to (it.getOrNull(1)?.toLongOrNull() ?: 0L) }
    val file = java.io.File(path)
    val apk = Updates.check(this, file, claimed = code)
    if (apk == null || apk.code <= (GatewayState.state.value.pendingApp?.code ?: ownVersionCode(this))) {
      file.delete()
      return
    }
    offerApp(core, apk)
    alertUpdate(apk)
  }

  private fun alertUpdate(apk: Updates.Apk) {
    val nm = getSystemService(NotificationManager::class.java)
    nm.createNotificationChannel(
      NotificationChannel(UPDATE_CHANNEL_ID, "Güncellemeler", NotificationManager.IMPORTANCE_DEFAULT).apply {
        description = "AleShare'in yeni sürümü yayımlanınca ya da bağlı bir cihaz yeni sürümü getirince haber verir."
      }
    )
    val open = PendingIntent.getActivity(this, 0, Intent(this, MainActivity::class.java), PendingIntent.FLAG_IMMUTABLE)
    val n = NotificationCompat.Builder(this, UPDATE_CHANNEL_ID)
      .setSmallIcon(R.drawable.ic_stat_hotspot)
      .setColor(ContextCompat.getColor(this, R.color.accent))
      .setContentTitle("AleShare ${apk.name} hazır")
      .setContentText("Bağlı bir cihaz daha yeni sürümü getirdi. Kurmak için dokun.")
      .setContentIntent(open)
      .setAutoCancel(true)
      .setCategory(NotificationCompat.CATEGORY_RECOMMENDATION)
      .build()
    runCatching { nm.notify(UPDATE_NOTIFICATION_ID, n) }
  }

  /** A notification of its own that the daily limit is close, or reached and sharing stopped. */
  private fun alertUsage(stopped: Boolean) {
    val nm = getSystemService(NotificationManager::class.java)
    nm.createNotificationChannel(
      NotificationChannel(USAGE_CHANNEL_ID, "Veri sınırı", NotificationManager.IMPORTANCE_DEFAULT).apply {
        description = "Günlük veri sınırına yaklaşınca ve sınır dolunca haber verir."
      }
    )
    val used = humanBytes(DataUsage.today(this).total)
    val limit = limitLabel(DataUsage.limitMb(this))
    val open = PendingIntent.getActivity(this, 0, Intent(this, MainActivity::class.java), PendingIntent.FLAG_IMMUTABLE)
    val n = NotificationCompat.Builder(this, USAGE_CHANNEL_ID)
      .setSmallIcon(R.drawable.ic_stat_hotspot)
      .setColor(ContextCompat.getColor(this, R.color.accent))
      .setContentTitle(if (stopped) "Günlük veri sınırı doldu" else "Günlük veri sınırı dolmak üzere")
      .setContentText(
        if (stopped) "Bugün $used kullanıldı, paylaşım durduruldu. Yeniden başlatırsan bugün bir daha durmaz."
        else "Bugün $used / $limit kullanıldı. Sınır dolunca paylaşım durur."
      )
      .setStyle(NotificationCompat.BigTextStyle())
      .setContentIntent(open)
      .setAutoCancel(true)
      .setCategory(NotificationCompat.CATEGORY_STATUS)
      .build()
    runCatching { nm.notify(USAGE_NOTIFICATION_ID, n) }
  }

  /** Shows [note] unless it says what is shown already. */
  private fun show(note: LiveNote) {
    if (note == shownNote) return
    shownNote = note
    getSystemService(NotificationManager::class.java).notify(NOTIFICATION_ID, notification(note))
  }

  /** The sharing notification (see liveNotification): the devices, the speed now, what was shared; and Durdur. */
  private fun notification(note: LiveNote): Notification {
    val channel = NotificationChannel(CHANNEL_ID, "Paylaşım", NotificationManager.IMPORTANCE_LOW).apply {
      description = "Paylaşım açıkken görünür: bağlı cihazlar, anlık hız ve paylaşılan veri."
      setShowBadge(false)
    }
    getSystemService(NotificationManager::class.java).createNotificationChannel(channel)
    val open = PendingIntent.getActivity(this, 0, Intent(this, MainActivity::class.java), PendingIntent.FLAG_IMMUTABLE)
    val stop = PendingIntent.getService(
      this, 1, Intent(this, GatewayService::class.java).setAction(ACTION_STOP), PendingIntent.FLAG_IMMUTABLE
    )
    return liveNotification(this, CHANNEL_ID, note, sharedSince, open, stop)
  }
  companion object {
    const val DEFAULT_PORT = 51820
    private const val ACTION_STOP = "dev.hotspot.app.STOP"
    /** Starts sharing, or stops it when it runs: the home screen widget's tap. */
    const val ACTION_TOGGLE = "dev.hotspot.app.TOGGLE_SHARING"
    private const val ACTION_CHANGE_WIFI = "dev.hotspot.app.CHANGE_WIFI"
    private const val ACTION_SWITCH_NOW = "dev.hotspot.app.SWITCH_WIFI_NOW"
    private const val ACTION_CANCEL_CHANGE = "dev.hotspot.app.CANCEL_WIFI_CHANGE"
    private const val ACTION_CHANGE_BAND = "dev.hotspot.app.CHANGE_BAND"
    private const val EXTRA_PORT = "port"
    private const val EXTRA_OWN_NETWORK = "own_network"
    private const val CHANNEL_ID = "gateway"
    private const val NOTIFICATION_ID = 1
    private const val USAGE_CHANNEL_ID = "usage"
    private const val USAGE_NOTIFICATION_ID = 4
    private const val UPDATE_CHANNEL_ID = "updates"
    private const val UPDATE_NOTIFICATION_ID = 5
    private const val STOPPED_NOTIFICATION_ID = 6
    private const val GUARD_CHANNEL_ID = "guard"
    private const val GUARD_NOTIFICATION_ID = 7
    private const val PLAN_NOTIFICATION_ID = 8
    private const val GUEST_NOTIFICATION_ID = 9
    private const val PLAN_EVERY_MILLIS = 60_000L
    private const val DEVICE_LIMIT_NOTIFICATION_BASE = 100
    const val WIFI_OFF_MESSAGE = "Wi-Fi kapatıldığı için paylaşım durdu. Wi-Fi'yi açıp yeniden başlat."
    private const val MAX_REOPEN = 3

    /** The longest the old network stays after a change: a computer asks once a minute. */
    private const val CHANGE_WAIT_MILLIS = 90_000L
    private const val SETTLE_MILLIS = 3_000L
    private const val NOTIFY_EVERY_MILLIS = 3_000L
    /**
     * A device counts as using the tunnel while something came from it this recently: a phone or tablet sends a
     * keepalive every 25 seconds, a computer checks the phone once a minute.
     */
    private const val ACTIVE_SECONDS = 75L

    /** [ownNetwork]: create the Wi-Fi network (Wi-Fi Direct) too, instead of relying on one opened by hand. */
    fun start(context: Context, port: Int = DEFAULT_PORT, ownNetwork: Boolean = false) {
      ContextCompat.startForegroundService(
        context,
        Intent(context, GatewayService::class.java).putExtra(EXTRA_PORT, port).putExtra(EXTRA_OWN_NETWORK, ownNetwork),
      )
    }

    fun stop(context: Context) {
      context.startService(Intent(context, GatewayService::class.java).setAction(ACTION_STOP))
    }

    /** Whether sharing was on when the app's process ended: so it was when an update replaced the app. */
    fun wasSharing(context: Context): Boolean = prefs(context).getBoolean(KEY_SHARING_ON, false)

    fun forgetSharing(context: Context) = setSharingOn(context, false)

    private fun setSharingOn(context: Context, on: Boolean) {
      prefs(context).edit().putBoolean(KEY_SHARING_ON, on).apply()
    }

    private fun prefs(context: Context) = context.getSharedPreferences("settings", Context.MODE_PRIVATE)

    private const val KEY_SHARING_ON = "sharing_on"

    /** Starts replacing the network with [next]; the secret stays in encrypted storage, not in the intent. */
    fun changeWifi(context: Context, next: WifiCredentials) {
      WifiNetwork.setNext(context, next)
      context.startService(Intent(context, GatewayService::class.java).setAction(ACTION_CHANGE_WIFI))
    }

    /** Replaces the network now, without waiting for more devices to learn it. */
    fun switchWifiNow(context: Context) {
      context.startService(Intent(context, GatewayService::class.java).setAction(ACTION_SWITCH_NOW))
    }

    fun cancelWifiChange(context: Context) {
      context.startService(Intent(context, GatewayService::class.java).setAction(ACTION_CANCEL_CHANGE))
    }

    /** Moves a running network to the band now chosen (Controls.band5); a stopped one opens on it next time. */
    fun changeBand(context: Context) {
      val s = GatewayState.state.value
      if (s.running && s.network != null) {
        runCatching { context.startService(Intent(context, GatewayService::class.java).setAction(ACTION_CHANGE_BAND)) }
      }
    }
  }
}

internal fun wifiGoingOff(state: Int): Boolean =
  state == WifiManager.WIFI_STATE_DISABLING || state == WifiManager.WIFI_STATE_DISABLED

/** A device counts as connected when its tunnel shook hands this recently (WireGuard renews every 2 minutes). */
private const val CONNECTED_SECONDS = 180L
private const val KEY_FOLLOWERS = "wifi_followers"

/**
 * The connected devices that follow a change of the network by themselves: those that have asked the phone about
 * its network at least once, in this run or an earlier one (their names are kept, nothing else). A device with
 * the official WireGuard app never asks, so a change does not wait for it.
 */
fun connectedFollowers(context: Context, core: Core): List<String> {
  val prefs = context.getSharedPreferences("settings", Context.MODE_PRIVATE)
  val known = prefs.getStringSet(KEY_FOLLOWERS, emptySet()).orEmpty()
  val peers = core.peerNames().lines().filter { it.isNotBlank() }.toSet()
  val followers = (known + core.wifiAskers().lines().filter { it.isNotBlank() }) intersect peers
  if (followers != known) prefs.edit().putStringSet(KEY_FOLLOWERS, followers).apply()
  return core.connectedPeers(CONNECTED_SECONDS).lines().filter { it.isNotBlank() && it in followers }
}

/**
 * Whether the first bytes of TLS handshakes leave the phone in pieces ("AleDPI modu"). Off unless the user turned it
 * on: it only helps where an operator's filter reads site names from the first segment, and it changes how every
 * secure connection starts.
 */
fun splitPreference(context: Context): Boolean =
  context.getSharedPreferences("settings", Context.MODE_PRIVATE).getBoolean("split_tls", false)

fun setSplitPreference(context: Context, on: Boolean) =
  context.getSharedPreferences("settings", Context.MODE_PRIVATE).edit().putBoolean("split_tls", on).apply()

/** Whether the app opens its own Wi-Fi network when sharing (the switch under "Gelişmiş"); on by default. */
fun ownNetworkPreference(context: Context): Boolean =
  context.getSharedPreferences("settings", Context.MODE_PRIVATE).getBoolean("own_network", true)
