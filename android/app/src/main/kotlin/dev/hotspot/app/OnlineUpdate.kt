package dev.hotspot.app

import android.app.NotificationChannel
import android.app.NotificationManager
import android.app.PendingIntent
import android.content.Context
import android.content.Intent
import androidx.core.app.NotificationCompat
import androidx.core.content.ContextCompat
import kotlinx.coroutines.flow.MutableStateFlow
import kotlinx.coroutines.flow.StateFlow
import kotlinx.coroutines.flow.asStateFlow
import kotlinx.coroutines.flow.update
import org.json.JSONObject
import java.io.File
import java.net.HttpURLConnection
import java.net.URL
import java.security.MessageDigest
import java.util.concurrent.Executors

/**
 * The project's releases on GitHub. Every release there carries the APK, the Windows program and "update.json",
 * which describes it; /releases/latest/download/ always leads to the newest release's file. The app looks there now
 * and then (a few kilobytes), tells the user when a newer release is out, and downloads it only when asked to.
 * Android's installer then takes the APK only if it carries this app's signature, and so does [Updates.check]
 * before the user is offered it.
 *
 * A downloaded APK is kept where the ones paired devices bring are kept ("received-…"), so the sharing phone
 * hands it on to its tablets like any other newer release, and it is thrown away once it is installed.
 */
object OnlineUpdate {
  const val REPO = "furkaanarslnn/AleShare"
  private const val MANIFEST = "https://github.com/$REPO/releases/latest/download/update.json"
  private const val DOWNLOADS = "https://github.com/$REPO/releases/download/"
  private const val MAX_BYTES = 256L shl 20 // no release file is anywhere near this
  private const val EVERY_MS = 12 * 3_600_000L // between two looks that got an answer
  private const val RETRY_MS = 30 * 60_000L // after one that got none: away from a network there is no news

  /** A release as update.json describes it, with the APK's place. */
  data class Release(val code: Long, val name: String, val url: String, val size: Long, val sha256: String)

  /** [offer]: a release newer than this app. [progress]: per cent downloaded. [file]: the APK, ready to install. */
  data class State(val offer: Release? = null, val progress: Int? = null, val file: String? = null, val error: String? = null, val checking: Boolean = false)

  private val _state = MutableStateFlow(State())
  val state: StateFlow<State> = _state.asStateFlow()

  private val worker = Executors.newSingleThreadExecutor { r -> Thread(r, "online-update").apply { isDaemon = true } }
  @Volatile private var nextLook = 0L // no need to read the preferences before this

  private const val KEY_ENABLED = "online_updates"
  private const val KEY_CHECKED = "online_checked"
  private const val KEY_TRIED = "online_tried"
  private const val KEY_MANIFEST = "online_manifest"
  private const val KEY_TOLD = "online_told"
  private const val CHANNEL_ID = "updates" // shared with GatewayService's "a device brought a newer release"
  private const val NOTIFICATION_ID = 10

  private fun prefs(context: Context) = context.getSharedPreferences("settings", Context.MODE_PRIVATE)

  /** Whether the app looks for newer releases by itself (the user's switch; on unless turned off). */
  fun enabled(context: Context): Boolean = prefs(context).getBoolean(KEY_ENABLED, true)

  fun setEnabled(context: Context, on: Boolean) {
    prefs(context).edit().putBoolean(KEY_ENABLED, on).apply()
    nextLook = 0
  }

  /** A release is worth offering when it is one of this project's and its APK lies among this project's releases. */
  internal fun valid(code: Long, name: String, url: String, size: Long, sha256: String): Boolean =
    code > 0 && name.isNotEmpty() && url.startsWith(DOWNLOADS) && size in 1..MAX_BYTES &&
      sha256.length == 64 && sha256.all { it in '0'..'9' || it in 'a'..'f' || it in 'A'..'F' }

  /** Whether it is time to look again, given when the last answer came and when the last try was made. */
  internal fun due(now: Long, checked: Long, tried: Long): Boolean =
    now < checked || now < tried || (now - checked >= EVERY_MS && now - tried >= RETRY_MS)

  private fun parse(text: String): Release? = runCatching {
    val o = JSONObject(text.removePrefix("\uFEFF"))
    val apk = o.getJSONObject("apk")
    val r = Release(
      code = o.getLong("code"),
      // What the release is called is the label; the name beside it is for the apps of release 1.1, which accept
      // only the name their own rule gives the number, and stands in when there is no label.
      name = o.optString("label").ifEmpty { o.getString("name") },
      url = apk.getString("url"),
      size = apk.getLong("size"),
      sha256 = apk.getString("sha256").lowercase(),
    )
    r.takeIf { valid(it.code, it.name, it.url, it.size, it.sha256) }
  }.getOrNull()

  /**
   * Looks for a newer release when it is time to (or now with [force], which also works with the switch off: the
   * user asked). [done] hears on the worker thread whether an answer came. Cheap to call often.
   */
  fun check(context: Context, force: Boolean = false, done: ((Boolean) -> Unit)? = null) {
    val app = context.applicationContext
    val now = System.currentTimeMillis()
    if (!force && (now < nextLook || !enabled(app))) return
    nextLook = now + 60_000 // while this look is under way
    worker.execute {
      val p = prefs(app)
      var answered = true
      if (force || due(now, p.getLong(KEY_CHECKED, 0), p.getLong(KEY_TRIED, 0))) {
        _state.update { it.copy(checking = true) }
        val text = runCatching { fetchText() }.getOrNull()
        answered = text != null && parse(text) != null
        val edit = p.edit().putLong(KEY_TRIED, now)
        if (answered) edit.putLong(KEY_CHECKED, now).putString(KEY_MANIFEST, text)
        edit.apply()
        _state.update { it.copy(checking = false) }
      }
      nextLook = now + (if (answered) 10 * 60_000L else 60_000L)
      publish(app)
      done?.invoke(answered)
    }
  }

  /** Shows the newest release known when it is newer than this app, and tells the user once about each. */
  private fun publish(context: Context) {
    val p = prefs(context)
    val own = ownVersionCode(context)
    val offer = p.getString(KEY_MANIFEST, null)?.let { parse(it) }?.takeIf { it.code > own }
    _state.update { s ->
      when {
        offer == null -> State()
        s.offer?.code == offer.code -> s.copy(offer = offer)
        else -> State(offer = offer, file = downloaded(context, offer)?.path)
      }
    }
    if (offer != null && offer.code > p.getLong(KEY_TOLD, 0)) {
      p.edit().putLong(KEY_TOLD, offer.code).apply()
      notify(context, offer)
    }
  }

  private fun target(context: Context, r: Release) = File(Updates.storeDir(context), "received-online-${r.code}.apk")

  /** The release's APK when it was downloaded before and is still the right file. */
  private fun downloaded(context: Context, r: Release): File? =
    target(context, r).takeIf { it.exists() && Updates.check(context, it, claimed = r.code) != null }

  private fun open(url: String): HttpURLConnection =
    (URL(url).openConnection() as HttpURLConnection).apply {
      connectTimeout = 15_000
      readTimeout = 30_000
      instanceFollowRedirects = true
      setRequestProperty("User-Agent", "AleShare-Android")
    }

  private fun fetchText(): String {
    val conn = open(MANIFEST)
    try {
      if (conn.responseCode != HttpURLConnection.HTTP_OK) error("GitHub ${conn.responseCode} dedi")
      return conn.inputStream.use { it.readNBytesCompat(64 * 1024) }.toString(Charsets.UTF_8)
    } finally {
      conn.disconnect()
    }
  }

  private fun java.io.InputStream.readNBytesCompat(max: Int): ByteArray {
    val out = java.io.ByteArrayOutputStream()
    val buf = ByteArray(8 * 1024)
    while (out.size() < max) {
      val n = read(buf, 0, minOf(buf.size, max - out.size()))
      if (n < 0) break
      out.write(buf, 0, n)
    }
    return out.toByteArray()
  }

  /** Downloads the offered release's APK and checks it; the screen then hands it to Android's installer. */
  fun download(context: Context) {
    val app = context.applicationContext
    val offer = _state.value.offer ?: return
    if (_state.value.progress != null) return
    _state.update { it.copy(progress = 0, file = null, error = null) }
    worker.execute {
      val result = runCatching {
        val out = target(app, offer)
        val part = File(out.path + ".part")
        val sum = MessageDigest.getInstance("SHA-256")
        var done = 0L
        val conn = open(offer.url)
        try {
          if (conn.responseCode != HttpURLConnection.HTTP_OK) error("GitHub ${conn.responseCode} dedi")
          var shown = 0
          conn.inputStream.use { input ->
            part.outputStream().use { output ->
              val buf = ByteArray(64 * 1024)
              while (true) {
                val n = input.read(buf)
                if (n < 0) break
                done += n
                if (done > offer.size) error("dosya beklenenden büyük")
                output.write(buf, 0, n)
                sum.update(buf, 0, n)
                val pct = (done * 100 / offer.size).toInt()
                if (pct != shown) {
                  shown = pct
                  _state.update { it.copy(progress = pct) }
                }
              }
            }
          }
        } finally {
          conn.disconnect()
        }
        val hex = sum.digest().joinToString("") { "%02x".format(it) }
        if (done != offer.size || hex != offer.sha256) {
          part.delete()
          error("indirme yarım kaldı ya da dosya bozuk; yeniden dene")
        }
        out.delete()
        if (!part.renameTo(out)) error("dosya kaydedilemedi")
        if (Updates.check(app, out, claimed = offer.code) == null) {
          out.delete()
          error("dosya AleShare'in imzasını taşımıyor")
        }
        out
      }
      _state.update { s ->
        s.copy(progress = null, file = result.getOrNull()?.path, error = result.exceptionOrNull()?.let { "İndirilemedi: ${it.message ?: it}" })
      }
    }
  }

  private fun notify(context: Context, r: Release) {
    val nm = context.getSystemService(NotificationManager::class.java)
    nm.createNotificationChannel(
      NotificationChannel(CHANNEL_ID, "Güncellemeler", NotificationManager.IMPORTANCE_DEFAULT).apply {
        description = "AleShare'in yeni sürümü yayımlanınca ya da bağlı bir cihaz yeni sürümü getirince haber verir."
      }
    )
    val open = PendingIntent.getActivity(context, 0, Intent(context, MainActivity::class.java), PendingIntent.FLAG_IMMUTABLE)
    val n = NotificationCompat.Builder(context, CHANNEL_ID)
      .setSmallIcon(R.drawable.ic_stat_hotspot)
      .setColor(ContextCompat.getColor(context, R.color.accent))
      .setContentTitle("AleShare ${r.name} yayımlandı")
      .setContentText("İndirip kurmak için dokun (${humanBytes(r.size)}).")
      .setContentIntent(open)
      .setAutoCancel(true)
      .setCategory(NotificationCompat.CATEGORY_RECOMMENDATION)
      .build()
    runCatching { nm.notify(NOTIFICATION_ID, n) }
  }
}
