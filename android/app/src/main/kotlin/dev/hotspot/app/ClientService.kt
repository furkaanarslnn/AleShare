package dev.hotspot.app

import android.app.Notification
import android.app.NotificationChannel
import android.app.NotificationManager
import android.app.PendingIntent
import android.content.Context
import android.content.Intent
import android.content.pm.ServiceInfo
import android.net.ConnectivityManager
import android.net.Network
import android.net.NetworkCapabilities
import android.net.NetworkRequest
import android.net.VpnService
import android.net.wifi.WifiNetworkSpecifier
import android.os.Build
import android.os.Handler
import android.os.Looper
import android.os.ParcelFileDescriptor
import android.os.SystemClock
import android.util.Log
import androidx.core.app.NotificationCompat
import androidx.core.app.ServiceCompat
import androidx.core.content.ContextCompat
import dev.hotspot.wgcore.ClientTunnel
import dev.hotspot.wgcore.Join
import dev.hotspot.wgcore.Protector
import dev.hotspot.wgcore.Wgcore
import dev.hotspot.wgcore.WifiNetworks
import java.io.File
import java.net.HttpURLConnection
import java.net.URL
import java.util.concurrent.Executors

/**
 * The other side of sharing: this device joins the Wi-Fi network of a phone that shares and sends everything
 * through a WireGuard tunnel to it, so the internet comes from that phone's mobile data.
 *
 * It keeps the phone's network requested, so it joins by itself whenever the network is in range and lets go
 * when it is not, until the user stops it. The tunnel runs in the VPN service of this very class.
 */
class ClientService : VpnService() {
  private val handler = Handler(Looper.getMainLooper())
  private val connectivity by lazy { getSystemService(ConnectivityManager::class.java) }

  private var join: Join? = null
  /**
   * The Wi-Fi network asked for: the saved one, or the one the phone said it changes to. While a change is
   * announced the two take turns, so the device finds the phone under either name.
   */
  private var target: WifiCredentials? = null
  private var callback: ConnectivityManager.NetworkCallback? = null
  private var network: Network? = null
  private var tunnel: ClientTunnel? = null
  /** The phone's store, as the phone last named it (with its token): releases and the speed test go there. */
  private var store: String? = null

  private var loopbackApp: String? = null
  private var requestTimeout = 0L
  private var searches = 0
  /** Searches still to come that wait long enough for the system's dialog (after the network was lost). */
  private var patientSearches = 0

  private var openedAt = 0L
  private var lastRx = 0L
  private var lastRxAt = 0L
  private var txAtRx = 0L

  // Asking the phone about its Wi-Fi network blocks for up to a few seconds, so it runs off the main thread.
  private val asker = Executors.newSingleThreadExecutor()
  private var asking = false
  private var askedAt = 0L

  // What waits its turn on the main thread, each by name, so that each can be taken back on its own. (Taking
  // everything back at once, as closing the tunnel did, also threw away the system's answers about the network,
  // which arrive on the same thread, and the end of a question to the phone or of a download: the device then
  // never asked the phone again, or never looked for its network again.)
  private val watcher = Runnable { watch() }
  private val retry = Runnable { network?.let(::openTunnel) }
  private val search = Runnable { requestNetwork() }

  /**
   * Started by the app, by Android again after it had to end the process (no intent), by the boot receiver, or
   * by Android itself when this app is the always-on VPN. All of them mean the same: connect to the saved phone.
   */
  override fun onStartCommand(intent: Intent?, flags: Int, startId: Int): Int {
    if (intent?.action == ACTION_STOP) {
      setWanted(this, false)
      shutDown()
      stopSelf(startId) // this stop only: a start that was asked for after it goes on
      return START_NOT_STICKY
    }
    if (intent?.action == ACTION_DOWNLOAD_UPDATE) {
      if (join == null) {
        stopSelf()
        return START_NOT_STICKY
      }
      downloadUpdate()
      return START_STICKY
    }
    if (intent?.action == ACTION_SPEED_TEST) {
      if (join == null) {
        stopSelf()
        return START_NOT_STICKY
      }
      // This app's sockets are on the phone's Wi-Fi network, outside the tunnel: the phone measures the internet.
      val wifi = network
      SpeedTest.startThroughPhone(this, store.takeIf { wifi != null }) { url -> wifi!!.openConnection(URL(url)) as HttpURLConnection }
      return START_STICKY
    }
    if (intent?.action == ACTION_RELOAD) {
      // The saved connection got another Wi-Fi network (a scanned Wi-Fi code): look for that one now.
      if (join == null || loopbackApp != null) {
        if (join == null) stopSelf()
        return START_NOT_STICKY
      }
      reload()
      return START_STICKY
    }
    try {
      ServiceCompat.startForeground(
        this, NOTIFICATION_ID, notification(note(ClientPhase.Searching), ClientPhase.Searching), ServiceInfo.FOREGROUND_SERVICE_TYPE_SPECIAL_USE,
      )
    } catch (e: Exception) {
      // A restart in the background may be refused; the boot receiver or the user starts it again later.
      Log.w(TAG, "not allowed to run in the foreground now", e)
      stopSelf()
      return START_NOT_STICKY
    }
    if (join != null) return START_STICKY
    intent?.getStringExtra(EXTRA_LOOPBACK_APP)?.let {
      startLoopback(it)
      return START_NOT_STICKY
    }
    val saved = ClientStore.load(this)
    if (saved == null || Build.VERSION.SDK_INT < 29) {
      fail(if (saved == null) "Kayıtlı bağlantı yok" else "Bu özellik için Android 10 ya da üstü gerekir")
      return START_NOT_STICKY
    }
    setWanted(this, true)
    join = saved
    target = WifiCredentials(saved.ssid, saved.password)
    requestNetwork()
    return START_STICKY
  }

  /**
   * A test aid for one device: the tunnel runs to this phone's own gateway and carries only the traffic of [app],
   * and no Wi-Fi network is joined. It exercises the VPN and the tunnel on a real phone without a second one.
   */
  private fun startLoopback(app: String) {
    val core = CoreHolder.peek()
    if (core == null || !GatewayState.state.value.running) {
      fail("Paylaşım açık değil")
      return
    }
    core.removePeer(LOOPBACK_PEER)
    val config = core.addPeer(LOOPBACK_PEER, "127.0.0.1:${GatewayState.state.value.port}")
    val j = Wgcore.parseJoin(Wgcore.encodeJoin("loopback", "loopback-test", config))
    loopbackApp = app
    join = j
    ClientState.update { it.copy(ssid = j.ssid) }
    openTunnel(null)
  }

  /**
   * Asks Android for the phone's network ([target]). It joins it when it is in range, after the user has allowed it
   * once (for each name: a new name of the phone's network asks again).
   */
  private fun requestNetwork() {
    if (Build.VERSION.SDK_INT < 29) return
    handler.removeCallbacks(search)
    val t = target ?: return
    // Android looks for a network once per request and does not come back to it when the network is gone, so a
    // fresh request is made whenever the old one ran out (see [onSearchEnded]) or the network was lost.
    callback?.let { runCatching { connectivity.unregisterNetworkCallback(it) } }
    if (lastPublished != ClientPhase.Searching) publish(ClientPhase.Searching)
    ClientState.update { it.copy(ssid = t.ssid, error = null) }
    val specifier = try {
      WifiNetworkSpecifier.Builder().setSsid(t.ssid).setWpa2Passphrase(t.passphrase).build()
    } catch (e: IllegalArgumentException) {
      // A name or password Android does not take (a link put together by hand). The connection ends here: left
      // wanted, every start of the app would run into the same refusal.
      fail("Kayıtlı bağlantının Wi-Fi adı ya da parolası geçersiz. Paylaşan telefondan yeni bir kod al.")
      return
    }
    val request = NetworkRequest.Builder()
      .addTransportType(NetworkCapabilities.TRANSPORT_WIFI)
      // The phone's network has no internet of its own: only the tunnel gives it one.
      .removeCapability(NetworkCapabilities.NET_CAPABILITY_INTERNET)
      .setNetworkSpecifier(specifier)
      .build()
    val cb = object : ConnectivityManager.NetworkCallback() {
      override fun onAvailable(network: Network) = onWifi(network)

      override fun onLost(network: Network) = onWifiLost(network)

      override fun onUnavailable() = onSearchEnded()
    }
    callback = cb
    // The first request may show the system's "connect to this network?" dialog, so it waits long enough for a
    // person. So do the first few after the network was lost: Android remembers the user's yes for the network's
    // access point, and a phone that starts sharing again opens its network anew, most likely under another
    // address, so it asks again; a short request closed that dialog before anyone could answer it. Later ones are
    // short: while a request is open Android does not join the device's own saved networks, and a device whose
    // phone is gone must get back to its usual Wi-Fi.
    requestTimeout = searchTimeout(first = searches++ == 0, patientLeft = patientSearches)
    if (patientSearches > 0) patientSearches--
    try {
      connectivity.requestNetwork(request, cb, handler, requestTimeout.toInt())
    } catch (e: RuntimeException) {
      // Android refuses now and then (too many requests at once, say): look again after a pause.
      Log.w(TAG, "the request for the phone's network was refused", e)
      callback = null
      handler.postDelayed(search, LOOK_AGAIN_MILLIS)
    }
  }

  /**
   * The request ended without a network: not in range, a scan that found nothing, the request's time ran out, or
   * the user said no in the system's dialog. Android does not say which (a refusal could not be told apart even
   * by timing), so the answer is always to look again after a pause, which also leaves the device time to join
   * its own Wi-Fi. A user who does not want to connect presses "Durdur". Looking gets slower after a while.
   */
  private fun onSearchEnded() {
    val j = join
    val next = ClientStore.next(this)
    // Right after the phone switched, its new network is the one to find: look for it often (the first try may
    // fail on a stale scan that still lists the old group).
    val nextFirst = next != null && SystemClock.elapsedRealtime() < nextFirstUntil
    if (j != null) target = afterSearch(WifiCredentials(j.ssid, j.password), next, target, nextFirst)
    val pause = when {
      nextFirst -> LOOK_AGAIN_SWITCH_MILLIS
      searches < SEARCHES_BEFORE_SLOWER -> LOOK_AGAIN_MILLIS
      else -> LOOK_AGAIN_SLOW_MILLIS
    }
    handler.postDelayed(search, pause)
  }

  /** Until then, after losing the network while a change was announced, only the new network is looked for. */
  private var nextFirstUntil = 0L

  private fun onWifi(wifi: Network) {
    network = wifi
    searches = 1 // found: when it is lost again, look often at first
    nextFirstUntil = 0
    // Found the phone under its new name: that is the saved network from now on.
    val next = ClientStore.next(this)
    if (next != null && target == next) {
      ClientStore.updateWifi(this, next)?.let { join = it }
      ClientStore.setNext(this, null)
      Log.i(TAG, "the phone's network is now ${next.ssid}")
    }
    askedAt = 0 // ask the phone soon: it may have news
    openTunnel(wifi)
  }

  private fun onWifiLost(wifi: Network) {
    if (network != wifi) return
    closeTunnel()
    network = null
    patientSearches = PATIENT_SEARCHES_AFTER_LOSS
    publish(ClientPhase.Searching)
    // Not at once: the device first gets the chance to fall back to its own Wi-Fi (see [requestNetwork]).
    callback?.let { runCatching { connectivity.unregisterNetworkCallback(it) } }
    callback = null
    // A network that goes away while a change is announced is most likely the phone switching: look for the new one,
    // soon (the phone opens it within seconds).
    val next = ClientStore.next(this)
    if (next != null) {
      target = next
      nextFirstUntil = SystemClock.elapsedRealtime() + NEXT_FIRST_MILLIS
    }
    handler.postDelayed(search, if (next != null) LOOK_AGAIN_SWITCH_MILLIS else REJOIN_DELAY_MILLIS)
  }

  private fun reload() {
    val saved = ClientStore.load(this) ?: return fail("Kayıtlı bağlantı yok")
    closeTunnel()
    callback?.let { runCatching { connectivity.unregisterNetworkCallback(it) } }
    callback = null
    network = null
    join = saved
    target = WifiCredentials(saved.ssid, saved.password)
    searches = 0 // a new name: Android asks the user again, so the first search waits for them
    requestNetwork()
  }

  /** Asks the phone, now and then while connected, whether its network keeps its name and password. */
  private fun askWifi(wifi: Network, config: String) {
    asking = true
    askedAt = SystemClock.elapsedRealtime()
    asker.execute {
      val answer = runCatching { Wgcore.askWifi(config, NetworkProtector(wifi)) }.getOrNull()
      handler.post {
        asking = false
        if (answer != null && network == wifi) followWifi(answer)
      }
    }
  }

  /** The phone said what its network is, and what it becomes: remember both. */
  private fun followWifi(w: WifiNetworks) {
    val now = WifiCredentials(w.ssid, w.password)
    val j = join ?: return
    if (now.ssid != j.ssid || now.passphrase != j.password) {
      ClientStore.updateWifi(this, now)?.let { join = it }
      target = now
      Log.i(TAG, "the phone's network is now ${now.ssid}")
    }
    val next = if (w.nextSSID.isNotEmpty()) WifiCredentials(w.nextSSID, w.nextPassword) else null
    if (next != ClientStore.next(this)) {
      ClientStore.setNext(this, next)
      Log.i(TAG, if (next != null) "the phone's network changes to ${next.ssid}" else "no change of network any more")
    }
    if (w.updateURL.isNotEmpty()) store = w.updateURL
    // A newer app on the phone: offer it, keeping how far a download of the same release has come.
    val offered = w.appCode > ownVersionCode(this) && w.updateURL.isNotEmpty()
    if (!offered) File(cacheDir, "updates").takeIf { it.exists() }?.deleteRecursively() // installed, or not needed any more
    ClientState.update { s ->
      val kept = s.update?.takeIf { it.code == w.appCode }
      s.copy(update = if (offered) (kept?.copy(url = w.updateURL) ?: AppUpdate(w.appName, w.appCode, w.updateURL)) else null)
    }
    // This device runs a newer app than the phone has: take it there, for the phone and the other devices.
    val own = ownVersionCode(this)
    if (own > w.appCode && w.updateURL.isNotEmpty()) bringApp(own, w.updateURL)
  }

  /** The release this device last brought to the phone, and when: a phone that turned it away is not asked again soon. */
  private var broughtCode = 0L
  private var broughtAt = 0L

  /** Uploads this app's own APK to the phone over the phone's Wi-Fi network (see Core.SetAppUpdate). */
  private fun bringApp(code: Long, url: String) {
    val wifi = network ?: return
    val now = SystemClock.elapsedRealtime()
    if (code == broughtCode && now - broughtAt < BRING_AGAIN_MILLIS) return
    broughtCode = code
    broughtAt = now
    val apk = File(applicationInfo.sourceDir)
    asker.execute {
      val result = runCatching {
        val conn = wifi.openConnection(URL("$url&code=$code")) as HttpURLConnection
        try {
          conn.requestMethod = "POST"
          conn.doOutput = true
          conn.connectTimeout = 10_000
          conn.readTimeout = 60_000
          conn.setFixedLengthStreamingMode(apk.length())
          conn.setRequestProperty("Content-Type", "application/vnd.android.package-archive")
          conn.outputStream.use { out -> apk.inputStream().use { it.copyTo(out, 64 * 1024) } }
          conn.responseCode
        } finally {
          conn.disconnect()
        }
      }
      Log.i(TAG, "brought release $code to the phone: ${result.getOrElse { it.toString() }}")
    }
  }

  /**
   * Downloads the phone's APK over the phone's Wi-Fi network (this app's sockets stay outside its own VPN, and
   * that network is not the default one) into the cache. The screen then hands it to Android's installer, which
   * asks the user and accepts it only if it carries the same signature as this app.
   */
  private fun downloadUpdate() {
    val update = ClientState.state.value.update ?: return
    val wifi = network
    if (wifi == null || update.progress != null) return
    ClientState.update { it.copy(update = update.copy(progress = 0, file = null, error = null)) }
    asker.execute {
      val result = runCatching {
        val dir = File(cacheDir, "updates").apply { mkdirs() }
        val part = File(dir, "hotspot.apk.part")
        val conn = wifi.openConnection(URL(update.url)) as HttpURLConnection
        conn.connectTimeout = 10_000
        conn.readTimeout = 20_000
        try {
          if (conn.responseCode != HttpURLConnection.HTTP_OK) error("telefon ${conn.responseCode} dedi")
          val total = conn.contentLengthLong
          var done = 0L
          var shown = -1
          conn.inputStream.use { input ->
            part.outputStream().use { output ->
              val buf = ByteArray(64 * 1024)
              while (true) {
                val n = input.read(buf)
                if (n < 0) break
                output.write(buf, 0, n)
                done += n
                val pct = if (total > 0) (done * 100 / total).toInt() else 0
                if (pct != shown) {
                  shown = pct
                  handler.post { ClientState.update { s -> s.copy(update = s.update?.copy(progress = pct)) } }
                }
              }
            }
          }
          if (total > 0 && done != total) error("indirme yarım kaldı")
        } finally {
          conn.disconnect()
        }
        val apk = File(dir, "hotspot.apk")
        apk.delete()
        if (!part.renameTo(apk)) error("dosya kaydedilemedi")
        // Only this app, signed with this app's key, in the release the phone named, goes on to Android's
        // installer: the file came over the Wi-Fi network in the clear, and anything on it could have answered.
        if (Updates.check(this@ClientService, apk, claimed = update.code) == null) {
          apk.delete()
          error("dosya AleShare'in imzasını taşımıyor")
        }
        apk
      }
      handler.post {
        ClientState.update { s ->
          s.copy(
            update = s.update?.copy(
              progress = null,
              file = result.getOrNull()?.path,
              error = result.exceptionOrNull()?.let { "İndirilemedi: ${it.message ?: it}" },
            )
          )
        }
      }
    }
  }

  private fun openTunnel(wifi: Network?) {
    val j = join ?: return
    closeTunnel()
    publish(ClientPhase.Connecting)
    val vpn = try {
      Builder().apply {
        setSession(getString(R.string.app_name))
        setMtu(j.mtu.toInt())
        // This app's own sockets stay outside the VPN: that is the tunnel's way to the phone.
        loopbackApp?.let { addAllowedApplication(it) } ?: addDisallowedApplication(packageName)
        wifi?.let { setUnderlyingNetworks(arrayOf(it)) }
        j.addresses.cidrs().forEach { (ip, bits) -> addAddress(ip, bits) }
        j.dns.split(',').filter { it.isNotBlank() }.forEach { addDnsServer(it.trim()) }
        j.routes.cidrs().forEach { (ip, bits) -> addRoute(ip, bits) }
      }.establish()
    } catch (e: Exception) {
      fail(e.message ?: e.toString())
      return
    }
    if (vpn == null) {
      fail("VPN izni verilmedi")
      return
    }
    try {
      // The descriptor now belongs to the Go side, which closes it on Stop. Go makes its own system calls, so
      // Android's "use this network" settings never reach its sockets: the tunnel's socket is bound to the
      // phone's network explicitly. That network has no internet and is not the default one, so nothing else
      // would take the packets there.
      tunnel = Wgcore.startClient(j.config, vpn.detachFd().toLong(), wifi?.let(::NetworkProtector))
    } catch (e: Exception) {
      fail(e.message ?: e.toString())
      return
    }
    val now = SystemClock.elapsedRealtime()
    openedAt = now
    lastRx = 0
    lastRxAt = now
    txAtRx = 0
    handler.postDelayed(watcher, WATCH_MILLIS)
  }

  /** Once in a while: has the phone answered, and is it still there? */
  private fun watch() {
    val t = tunnel ?: return
    val now = SystemClock.elapsedRealtime()
    val rx = t.rxBytes()
    val tx = t.txBytes()
    val age = t.handshakeAge()
    if (rx != lastRx) {
      lastRx = rx
      lastRxAt = now
      txAtRx = tx
    }
    // A phone that is there answers the handshake at first. Later: an idle tunnel still renews its keys every two
    // minutes (a dead phone does not answer that), and one that is being used gets answers to what it sends, so
    // a lot going out with nothing coming back for a while is a dead phone too. Mere quiet is not: the phone
    // does not talk when there is nothing to say.
    val silent = when {
      age < 0 -> now - openedAt > CONNECT_TIMEOUT_MILLIS
      age > STALE_HANDSHAKE_SECONDS -> true
      else -> tx - txAtRx > UNANSWERED_BYTES && now - lastRxAt > UNANSWERED_MILLIS
    }
    if (silent) {
      // Give the normal internet back instead of leaving the tunnel to swallow everything, and try again later.
      closeTunnel()
      publish(ClientPhase.Unreachable)
      handler.postDelayed(retry, RETRY_MILLIS)
      return
    }
    if (age >= 0) {
      ClientState.update { it.copy(phase = ClientPhase.Connected, rx = rx, tx = t.txBytes()) }
      if (lastPublished != ClientPhase.Connected) {
        connectedSince = System.currentTimeMillis()
        publish(ClientPhase.Connected)
      }
      // The notification: the speed since the last look, and what came and went through the tunnel.
      val down = perSecond(rx, noteRx, now - noteAt)
      val up = perSecond(tx, noteTx, now - noteAt)
      noteRx = rx
      noteTx = tx
      noteAt = now
      show(clientNote(down, up, rx, tx), ClientPhase.Connected)
      if (++ticks % LOG_EVERY == 0) Log.i(TAG, "connected: rx=$rx tx=${t.txBytes()} handshake ${age}s ago")
      val wifi = network
      val j = join
      if (wifi != null && j != null && !asking && now - askedAt >= ASK_EVERY_MILLIS) askWifi(wifi, j.config)
    }
    handler.postDelayed(watcher, WATCH_MILLIS)
  }

  private var lastPublished: ClientPhase? = null
  private var ticks = 0

  /** The tunnel's counters at the notification's last look, for the speed now; null before the first. */
  private var noteRx: Long? = null
  private var noteTx: Long? = null
  private var noteAt = 0L
  /** When the phone first answered (epoch millis), for the notification's clock; 0 while not connected. */
  private var connectedSince = 0L
  private var shownNote: LiveNote? = null

  private fun publish(phase: ClientPhase) {
    lastPublished = phase
    Log.i(TAG, "$phase (${target?.ssid})")
    ClientState.update { it.copy(phase = phase, error = null) }
    show(note(phase), phase)
  }

  /** Shows [note] for [phase] unless it says what is shown already. */
  private fun show(note: LiveNote, phase: ClientPhase) {
    if (note == shownNote) return
    shownNote = note
    getSystemService(NotificationManager::class.java).notify(NOTIFICATION_ID, notification(note, phase))
  }

  private fun closeTunnel() {
    handler.removeCallbacks(watcher)
    handler.removeCallbacks(retry)
    tunnel?.stop()
    tunnel = null
    noteRx = null
    noteTx = null
    connectedSince = 0L
    ClientState.update { it.copy(rx = 0, tx = 0) }
  }

  private fun fail(message: String) {
    Log.w(TAG, "failed: $message")
    setWanted(this, false)
    shutDown()
    ClientState.update { it.copy(error = message) }
    stopSelf()
  }

  private fun shutDown() {
    closeTunnel()
    handler.removeCallbacksAndMessages(null) // everything: nothing of this connection is wanted any more
    asking = false // a question under way was just told not to report back
    if (loopbackApp != null) {
      loopbackApp = null
      CoreHolder.peek()?.let {
        it.removePeer(LOOPBACK_PEER)
        CoreHolder.save(this, it)
      }
    }
    callback?.let { runCatching { connectivity.unregisterNetworkCallback(it) } }
    callback = null
    network = null
    join = null
    target = null
    searches = 0
    askedAt = 0
    nextFirstUntil = 0
    lastPublished = null
    ClientState.update { ClientUiState() }
  }

  /**
   * The VPN was taken down from outside: another VPN app took its place, the user disconnected it in the system's
   * settings, or this app is being updated. Only the first clearly means "stay off"; after an update the
   * connection must come back, so the wish to be connected is kept unless the consent now belongs to another app.
   */
  override fun onRevoke() {
    if (prepare(this) != null) setWanted(this, false)
    shutDown()
    stopSelf()
  }

  override fun onDestroy() {
    shutDown()
    asker.shutdown()
    super.onDestroy()
  }

  /** What the notification says in [phase]; once connected, the speed and the totals take over (see watch). */
  private fun note(phase: ClientPhase): LiveNote {
    val ssid = target?.ssid ?: join?.ssid ?: "Telefonun ağı"
    return when (phase) {
      ClientPhase.Connected -> clientNote(0, 0, ClientState.state.value.rx, ClientState.state.value.tx)
      ClientPhase.Connecting -> LiveNote("Bağlanıyor", ssid)
      ClientPhase.Unreachable -> LiveNote("Telefon yanıt vermiyor", "Şimdilik kendi internetin kullanılıyor; yeniden denenecek.")
      else -> LiveNote("Telefon aranıyor", "$ssid ağı yakına gelince bağlanır.")
    }
  }

  /** The connection's notification (see liveNotification), with Durdur; the clock runs while connected. */
  private fun notification(note: LiveNote, phase: ClientPhase): Notification {
    val channel = NotificationChannel(CHANNEL_ID, "Bağlantı", NotificationManager.IMPORTANCE_LOW).apply {
      description = "Başka bir telefonun internetine bağlıyken görünür: anlık hız, indirilen ve yüklenen veri."
      setShowBadge(false)
    }
    getSystemService(NotificationManager::class.java).createNotificationChannel(channel)
    val open = PendingIntent.getActivity(this, 0, Intent(this, MainActivity::class.java), PendingIntent.FLAG_IMMUTABLE)
    val stop = PendingIntent.getService(
      this, 2, Intent(this, ClientService::class.java).setAction(ACTION_STOP), PendingIntent.FLAG_IMMUTABLE
    )
    return liveNotification(this, CHANNEL_ID, note, if (phase == ClientPhase.Connected) connectedSince else 0L, open, stop)
  }

  companion object {
    private const val TAG = "HotspotClient"
    private const val LOG_EVERY = 5
    private const val LOOPBACK_PEER = "loopback-test"
    const val EXTRA_LOOPBACK_APP = "loopback_app"
    private const val ACTION_STOP = "dev.hotspot.app.CLIENT_STOP"
    private const val ACTION_RELOAD = "dev.hotspot.app.CLIENT_RELOAD"
    private const val ACTION_DOWNLOAD_UPDATE = "dev.hotspot.app.CLIENT_DOWNLOAD_UPDATE"
    private const val ACTION_SPEED_TEST = "dev.hotspot.app.CLIENT_SPEED_TEST"
    private const val CHANNEL_ID = "client"
    private const val NOTIFICATION_ID = 2
    const val FIRST_SEARCH_TIMEOUT_MILLIS = 60_000L
    const val SEARCH_TIMEOUT_MILLIS = 12_000L
    /** After losing the phone's network: this many searches long enough to answer the system's dialog. */
    private const val PATIENT_SEARCHES_AFTER_LOSS = 3
    private const val LOOK_AGAIN_MILLIS = 20_000L
    private const val LOOK_AGAIN_SLOW_MILLIS = 60_000L
    private const val SEARCHES_BEFORE_SLOWER = 15
    private const val REJOIN_DELAY_MILLIS = 15_000L
    private const val LOOK_AGAIN_SWITCH_MILLIS = 5_000L
    private const val NEXT_FIRST_MILLIS = 120_000L
    private const val WATCH_MILLIS = 2_000L
    private const val CONNECT_TIMEOUT_MILLIS = 20_000L
    private const val STALE_HANDSHAKE_SECONDS = 200L
    private const val UNANSWERED_BYTES = 16 * 1024L
    private const val UNANSWERED_MILLIS = 20_000L
    private const val RETRY_MILLIS = 45_000L
    /** How often a connected device asks whether the phone's network is about to change (it waits at most 90 s). */
    private const val ASK_EVERY_MILLIS = 30_000L
    /** How long before this device brings the same release to the phone again, if the phone did not take it. */
    private const val BRING_AGAIN_MILLIS = 30 * 60_000L

    /** Starts it. The system's VPN consent ([VpnService.prepare]) must have been given before. */
    fun start(context: Context) {
      ContextCompat.startForegroundService(context, Intent(context, ClientService::class.java))
    }

    /** Whether the user left the connection on: then it comes back after a restart of the device. */
    fun wanted(context: Context): Boolean = prefs(context).getBoolean(KEY_WANTED, false)

    private fun setWanted(context: Context, on: Boolean) {
      prefs(context).edit().putBoolean(KEY_WANTED, on).apply()
      ShareControls.refresh(context) // a phone that uses another phone's internet cannot share
    }

    private fun prefs(context: Context) = context.getSharedPreferences("settings", Context.MODE_PRIVATE)

    private const val KEY_WANTED = "client_wanted"

    fun stop(context: Context) {
      context.startService(Intent(context, ClientService::class.java).setAction(ACTION_STOP))
    }

    /** Makes a running connection use the saved one again, after its Wi-Fi network was changed. */
    fun reload(context: Context) {
      context.startService(Intent(context, ClientService::class.java).setAction(ACTION_RELOAD))
    }

    /** Downloads the newer app the phone offers (see [ClientUiState.update]); the connection must be running. */
    fun downloadUpdate(context: Context) {
      runCatching { context.startService(Intent(context, ClientService::class.java).setAction(ACTION_DOWNLOAD_UPDATE)) }
    }

    /** Measures the Wi-Fi to the phone and has the phone measure its internet (see SpeedTest.startThroughPhone). */
    fun speedTest(context: Context) {
      runCatching { context.startService(Intent(context, ClientService::class.java).setAction(ACTION_SPEED_TEST)) }
    }
  }
}

/**
 * The network to look for after a search for [current] ended without it. While the phone has announced that it
 * changes to [next]: only the new one for a while after the phone's network was lost ([nextFirst]), then both
 * names in turn. Otherwise the [saved] one (the same as [current]).
 */
internal fun afterSearch(saved: WifiCredentials, next: WifiCredentials?, current: WifiCredentials?, nextFirst: Boolean): WifiCredentials =
  when {
    next == null -> current ?: saved
    nextFirst -> next
    current == next -> saved
    else -> next
  }

/** Binds a socket of the Go tunnel to [network], as [CellularProtector] does for the gateway's sockets. */
private class NetworkProtector(private val network: Network) : Protector {
  override fun protect(fd: Long): Boolean =
    try {
      // fromFd duplicates the descriptor; the socket option set on it applies to the original socket.
      ParcelFileDescriptor.fromFd(fd.toInt()).use { network.bindSocket(it.fileDescriptor) }
      true
    } catch (_: Exception) {
      false
    }
}

/** "10.8.0.2/32,fd42::2/128" as pairs of address and prefix length. */
private fun String.cidrs(): List<Pair<String, Int>> =
  split(',').map { it.trim() }.filter { it.contains('/') }.map { it.substringBefore('/') to it.substringAfter('/').toInt() }

/** How long one search for the phone's network waits: long enough for the system's dialog when it may come. */
internal fun searchTimeout(first: Boolean, patientLeft: Int): Long =
  if (first || patientLeft > 0) ClientService.FIRST_SEARCH_TIMEOUT_MILLIS else ClientService.SEARCH_TIMEOUT_MILLIS
