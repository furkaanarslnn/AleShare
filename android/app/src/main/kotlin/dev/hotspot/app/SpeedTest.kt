package dev.hotspot.app

import android.content.Context
import kotlinx.coroutines.flow.MutableStateFlow
import kotlinx.coroutines.flow.StateFlow
import kotlinx.coroutines.flow.asStateFlow
import kotlinx.coroutines.flow.update
import org.json.JSONObject
import java.net.HttpURLConnection
import java.net.URL
import kotlin.concurrent.thread

/**
 * The speed test, one at a time, started from the app or from a paired computer's window. On the sharing phone it
 * measures the phone's own internet (see Core.speedTest). On a device connected to a sharing phone (a tablet) its
 * own sockets stay outside its tunnel, on the phone's Wi-Fi network, which leads nowhere else: it measures that
 * Wi-Fi to the phone, then asks the phone to measure its mobile internet, as a paired computer does. The last
 * result is kept across restarts.
 */
object SpeedTest {
  /**
   * Rates in bits per second; [bytes] the mobile data the test used; [at] in Unix seconds. [wifiDownBps] and
   * [wifiUpBps] are the Wi-Fi between this device and the sharing phone, 0 when not measured (on the phone itself).
   */
  data class Result(
    val downBps: Long,
    val upBps: Long,
    val pingMs: Long,
    val bytes: Long,
    val at: Long,
    val wifiDownBps: Long = 0,
    val wifiUpBps: Long = 0,
  )

  /** [stage] says what is being measured while it runs. */
  data class State(val running: Boolean = false, val last: Result? = null, val error: String? = null, val stage: String? = null)

  private val _state = MutableStateFlow(State())
  val state: StateFlow<State> = _state.asStateFlow()
  private var loaded = false

  /** Reads the last result kept, once. */
  @Synchronized
  fun load(context: Context) {
    if (loaded) return
    loaded = true
    val last = prefs(context).getString(KEY_LAST, null)?.let { parse(it) }
    _state.update { it.copy(last = last) }
  }

  /** Starts a test unless one runs; its result arrives in [state]. */
  @Synchronized
  fun start(context: Context) {
    load(context)
    if (_state.value.running) return
    // Connected to a sharing phone, not sharing itself: the service that holds the phone's network measures.
    if (ClientState.state.value.phase == ClientPhase.Connected && !GatewayState.state.value.running) {
      ClientService.speedTest(context)
      return
    }
    _state.update { it.copy(running = true, error = null, stage = "Mobil internet ölçülüyor…") }
    val app = context.applicationContext
    thread(name = "speedtest", isDaemon = true) {
      val outcome = runCatching { CoreHolder.get(app).speedTest() }
      val result = outcome.getOrNull()?.let { parse(it) }
      if (result != null) prefs(app).edit().putString(KEY_LAST, outcome.getOrNull()).apply()
      _state.update {
        State(running = false, last = result ?: it.last, error = if (result == null) "Ölçülemedi: internete ulaşılamadı." else null)
      }
    }
  }

  /**
   * The test of a device connected to a sharing phone: [store] is the phone's store as the phone gave it (any of its
   * URLs, with the token), [open] opens a connection on the phone's Wi-Fi network.
   */
  @Synchronized
  fun startThroughPhone(context: Context, store: String?, open: (String) -> HttpURLConnection) {
    load(context)
    if (_state.value.running) return
    val s = store?.let { storeOf(it) }
    if (s == null) {
      _state.update { it.copy(error = "Telefona henüz ulaşılamadı; biraz sonra yeniden dene.") }
      return
    }
    val (base, token) = s
    _state.update { it.copy(running = true, error = null, stage = "Telefonla aradaki Wi-Fi ölçülüyor…") }
    val app = context.applicationContext
    thread(name = "speedtest", isDaemon = true) {
      val outcome = runCatching {
        val (wifiDown, wifiUp) = wifiRates(base, token, open)
        _state.update { it.copy(stage = "Telefon mobil interneti ölçüyor…") }
        phoneRates(base, token, open).copy(wifiDownBps = wifiDown, wifiUpBps = wifiUp)
      }
      outcome.getOrNull()?.let { prefs(app).edit().putString(KEY_LAST, json(it)).apply() }
      _state.update {
        State(running = false, last = outcome.getOrNull() ?: it.last, error = outcome.exceptionOrNull()?.let { e -> "Ölçülemedi: ${e.message ?: e}" })
      }
    }
  }

  /** Down and up between this device and the phone (the phone's GET and POST /speed), bits per second. */
  private fun wifiRates(base: String, token: String, open: (String) -> HttpURLConnection): Pair<Long, Long> {
    val down = open("$base/speed?t=$token&bytes=$WIFI_MAX_BYTES").run {
      connectTimeout = 5_000
      readTimeout = 10_000
      try {
        if (responseCode != HttpURLConnection.HTTP_OK) error("telefon hız ölçümünü bilmiyor; telefondaki AleShare'i güncelle")
        val start = System.nanoTime()
        val end = start + WIFI_NANOS
        var n = 0L
        val buf = ByteArray(64 * 1024)
        inputStream.use { input ->
          while (System.nanoTime() < end) {
            val r = input.read(buf)
            if (r < 0) break
            n += r
          }
        }
        n * 8 * 1_000_000_000 / (System.nanoTime() - start).coerceAtLeast(1)
      } finally {
        disconnect()
      }
    }
    val up = open("$base/speed?t=$token").run {
      connectTimeout = 5_000
      readTimeout = 10_000
      requestMethod = "POST"
      doOutput = true
      setChunkedStreamingMode(64 * 1024)
      try {
        val end = System.nanoTime() + WIFI_NANOS
        val buf = ByteArray(64 * 1024)
        outputStream.use { out -> while (System.nanoTime() < end) out.write(buf) }
        val o = JSONObject(inputStream.bufferedReader().use { it.readText() })
        o.getLong("bytes") * 8 * 1000 / o.getLong("ms").coerceAtLeast(1)
      } finally {
        disconnect()
      }
    }
    return down to up
  }

  /** Asks the phone to measure its mobile internet and waits for the result (GET and POST /phone). */
  private fun phoneRates(base: String, token: String, open: (String) -> HttpURLConnection): Result {
    fun status(): JSONObject? = runCatching {
      open("$base/phone?t=$token").run {
        connectTimeout = 5_000
        readTimeout = 5_000
        try {
          JSONObject(inputStream.bufferedReader().use { it.readText() }).optJSONObject("speedtest")
        } finally {
          disconnect()
        }
      }
    }.getOrNull()
    val before = status()?.optLong("at") ?: 0
    open("$base/phone?t=$token").run {
      connectTimeout = 5_000
      readTimeout = 5_000
      requestMethod = "POST"
      doOutput = true
      setRequestProperty("Content-Type", "application/json")
      try {
        outputStream.use { it.write("""{"speedtest":true}""".toByteArray()) }
        if (responseCode != HttpURLConnection.HTTP_OK) error("telefon $responseCode dedi")
      } finally {
        disconnect()
      }
    }
    val asked = System.currentTimeMillis()
    while (System.currentTimeMillis() - asked < PHONE_WAIT_MILLIS) {
      Thread.sleep(2_000)
      val s = status() ?: continue
      if (s.optBoolean("running")) continue
      if (s.optLong("at") > before) {
        return Result(s.getLong("down_bps"), s.getLong("up_bps"), s.getLong("ping_ms"), s.getLong("bytes"), s.getLong("at"))
      }
      // The phone looks for requests every three seconds: an error before then is an older one.
      if (s.optString("error").isNotEmpty() && System.currentTimeMillis() - asked > 8_000) error(s.optString("error").removePrefix("Ölçülemedi: "))
    }
    error("telefon mobil hızı ölçemedi")
  }

  private fun parse(json: String): Result? = runCatching {
    val o = JSONObject(json)
    Result(
      o.getLong("down_bps"), o.getLong("up_bps"), o.getLong("ping_ms"), o.getLong("bytes"), o.getLong("at"),
      o.optLong("wifi_down_bps"), o.optLong("wifi_up_bps"),
    )
  }.getOrNull()

  private fun json(r: Result): String = JSONObject()
    .put("down_bps", r.downBps).put("up_bps", r.upBps).put("ping_ms", r.pingMs).put("bytes", r.bytes).put("at", r.at)
    .put("wifi_down_bps", r.wifiDownBps).put("wifi_up_bps", r.wifiUpBps)
    .toString()

  private fun prefs(context: Context) = context.getSharedPreferences("settings", Context.MODE_PRIVATE)

  private const val KEY_LAST = "speedtest_last"
  private const val WIFI_MAX_BYTES = 400L shl 20
  private const val WIFI_NANOS = 4_000_000_000L
  private const val PHONE_WAIT_MILLIS = 75_000L
}

/** The phone's store from one of its URLs ("http://192.168.49.1:51823/hotspot.apk?t=…"): its base and the token. */
internal fun storeOf(url: String): Pair<String, String>? = runCatching {
  val u = URL(url)
  val token = u.query?.split('&')?.firstOrNull { it.startsWith("t=") }?.removePrefix("t=")?.takeIf { it.isNotEmpty() }
  token?.let { "${u.protocol}://${u.authority}" to it }
}.getOrNull()

/** Where a measured link is slow, comparing the phone's mobile download with the Wi-Fi to it. */
internal fun bottleneck(r: SpeedTest.Result): String? = when {
  r.wifiDownBps <= 0 -> null
  r.downBps < r.wifiDownBps * 8 / 10 -> "Yavaşlık mobil internetten kaynaklanıyor; Wi-Fi daha hızlı."
  r.wifiDownBps < r.downBps * 8 / 10 -> "Yavaşlık Wi-Fi'den kaynaklanıyor. Telefona yaklaş ya da 5 GHz'e geç."
  else -> "Wi-Fi ile mobil internet aynı hızda."
}

/** A rate of bits per second the way people read it: "31,2 Mbit/s", or kilobits under one megabit. */
fun bitRate(bps: Long): String = when {
  bps >= 100_000_000 -> "${bps / 1_000_000} Mbit/s"
  bps >= 1_000_000 -> String.format(Tr.locale, "%.1f Mbit/s", bps / 1e6)
  else -> "${bps / 1000} kbit/s"
}
