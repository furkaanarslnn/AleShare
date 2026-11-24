package dev.hotspot.app

import android.Manifest
import android.annotation.SuppressLint
import android.content.Context
import android.content.pm.PackageManager
import android.net.wifi.WifiManager
import android.net.wifi.p2p.WifiP2pConfig
import android.net.wifi.p2p.WifiP2pManager
import android.os.Build
import android.os.Looper
import androidx.annotation.RequiresApi
import androidx.core.content.ContextCompat
import java.security.SecureRandom

/** Name and password of the Wi-Fi network this app creates. Fixed, so a computer joins once and remembers it. */
data class WifiCredentials(val ssid: String, val passphrase: String)

/** Why sharing on the app's own network cannot start: the network is Wi-Fi Direct, on the Wi-Fi radio. */
const val WIFI_OFF_TO_START = "Wi-Fi kapalı. Paylaşım telefonun kendi Wi-Fi ağıyla yapılır: Wi-Fi'yi aç, sonra yeniden başlat."

object WifiNetwork {
  private const val ENTRY = "wifi"
  private const val NEXT_ENTRY = "wifi_next"

  /**
   * Android names a Wi-Fi Direct group "DIRECT-xy" and something after it (xy two letters or digits), 32 bytes at
   * most, and refuses any other name. The user picks what comes after "DIRECT-", so its first two characters are the
   * xy. (Up to 1.0.30 the app put "HS-" in between: such names stay valid.)
   */
  const val PREFIX = "DIRECT-"
  const val OLD_PREFIX = "DIRECT-HS-"
  const val MAX_NAME = 32 - PREFIX.length
  const val MIN_PASSWORD = 8
  const val MAX_PASSWORD = 63

  // No look-alikes (0/O, 1/l/I): the password is read from a screen and typed once.
  private const val PASSWORD_ALPHABET = "abcdefghjkmnpqrstuvwxyzABCDEFGHJKLMNPQRSTUVWXYZ23456789"
  private const val SSID_ALPHABET = "ABCDEFGHJKLMNPQRSTUVWXYZ23456789"

  /** A network for this run of the app only, while the saved one cannot be read (see [credentials]). */
  private var temporary: WifiCredentials? = null

  /** The credentials; made up on first use and kept encrypted. */
  @Synchronized
  fun credentials(context: Context): WifiCredentials {
    read(context, ENTRY)?.let { return it }
    // Saved, but the keystore cannot read it today (see SecureStore): a network for this run only. The saved one,
    // which the paired devices know, stays where it is for the day the keystore reads it again.
    if (SecureStore.unreadable(ENTRY)) {
      return temporary ?: WifiCredentials(PREFIX + random(SSID_ALPHABET, 4), randomPassword()).also { temporary = it }
    }
    val created = WifiCredentials(PREFIX + random(SSID_ALPHABET, 4), randomPassword())
    write(context, ENTRY, created)
    return created
  }

  fun randomPassword(): String = random(PASSWORD_ALPHABET, 16)

  private fun random(alphabet: String, n: Int): String {
    val random = SecureRandom()
    return String(CharArray(n) { alphabet[random.nextInt(alphabet.length)] })
  }

  private fun read(context: Context, entry: String): WifiCredentials? =
    SecureStore.get(context, entry)?.lines()?.takeIf { it.size >= 2 && it[0].startsWith("DIRECT-") }?.let { WifiCredentials(it[0], it[1]) }

  private fun write(context: Context, entry: String, c: WifiCredentials) = SecureStore.put(context, entry, c.ssid + "\n" + c.passphrase)

  /**
   * Why a name (the part after [PREFIX]) cannot be used, or null if it can. ASCII letters, digits, '-' and '_'
   * only: a Windows computer reads network names through a console that garbles anything else. The first two are
   * a letter or a digit each: Android's rule for the "xy" of "DIRECT-xy".
   */
  fun nameProblem(name: String): String? = when {
    name.isEmpty() -> "Bir ad yaz."
    name.length > MAX_NAME -> "En çok $MAX_NAME karakter."
    !name.all { asciiAlnum(it) || it == '-' || it == '_' } -> "Yalnızca harf (Türkçe karaktersiz), rakam, - ve _ kullan."
    name.length < 2 || !asciiAlnum(name[0]) || !asciiAlnum(name[1]) -> "İlk iki karakter harf ya da rakam olmalı."
    else -> null
  }

  private fun asciiAlnum(c: Char) = c in 'a'..'z' || c in 'A'..'Z' || c in '0'..'9'

  /** Why a password cannot be used, or null if it can: WPA2 wants 8 to 63 printable ASCII characters. */
  fun passwordProblem(password: String): String? = when {
    password.length < MIN_PASSWORD -> "En az $MIN_PASSWORD karakter."
    password.length > MAX_PASSWORD -> "En çok $MAX_PASSWORD karakter."
    !password.all { it in ' '..'~' } -> "Türkçe karakter kullanma (ğ, ü, ş, ı, ö, ç)."
    else -> null
  }

  /**
   * The network that replaces the current one once the paired devices have learned it (see GatewayService). Kept
   * encrypted until then; null when no change is under way.
   */
  fun next(context: Context): WifiCredentials? = read(context, NEXT_ENTRY)

  fun setNext(context: Context, next: WifiCredentials) = write(context, NEXT_ENTRY, next)

  fun clearNext(context: Context) = SecureStore.remove(context, NEXT_ENTRY)

  /** Makes the network that follows the current one, and returns it. */
  fun commitNext(context: Context): WifiCredentials? {
    val next = next(context) ?: return null
    write(context, ENTRY, next)
    clearNext(context)
    return next
  }

  /** Puts [c] back as the current network (a change that could not be carried out). */
  fun restore(context: Context, c: WifiCredentials) = write(context, ENTRY, c)

  /** What a phone's camera reads to join the network. */
  fun qrText(c: WifiCredentials) = "WIFI:T:WPA;S:${escape(c.ssid)};P:${escape(c.passphrase)};;"

  // The format of these codes gives \ ; , : and " a special meaning.
  private fun escape(s: String) = s.replace(Regex("""([\\;,:"])"""), """\\$1""")

  /** Reads a Wi-Fi code ("WIFI:T:WPA;S:…;P:…;;"), or null if [text] is not one. */
  fun parseQr(text: String): WifiCredentials? {
    if (!text.startsWith("WIFI:")) return null
    val fields = mutableMapOf<String, String>()
    var key = StringBuilder()
    var value = StringBuilder()
    var inValue = false
    var i = "WIFI:".length
    while (i < text.length) {
      val ch = text[i]
      when {
        ch == '\\' && i + 1 < text.length -> { (if (inValue) value else key).append(text[i + 1]); i++ }
        ch == ':' && !inValue -> inValue = true
        ch == ';' -> {
          if (key.isNotEmpty()) fields[key.toString()] = value.toString()
          key = StringBuilder(); value = StringBuilder(); inValue = false
        }
        else -> (if (inValue) value else key).append(ch)
      }
      i++
    }
    val ssid = fields["S"] ?: return null
    val password = fields["P"] ?: return null
    return WifiCredentials(ssid, password).takeIf { ssid.startsWith("DIRECT-") && passwordProblem(password) == null }
  }
}

/**
 * The Wi-Fi Direct group that is the network of the computers. It never touches a
 * group that belongs to someone else: another app's group is reported, not removed.
 */
@SuppressLint("MissingPermission")
class WifiDirectGroup(context: Context) {
  private val appContext = context.applicationContext
  private val manager = context.getSystemService(Context.WIFI_P2P_SERVICE) as? WifiP2pManager
  private val channel = manager?.initialize(context, Looper.getMainLooper(), null)
  private val wifi = context.applicationContext.getSystemService(WifiManager::class.java)

  /** The group's frequency in MHz (2412…, 5180…) as the system tells it; [result] gets 0 when there is none. */
  fun frequency(result: (Int) -> Unit) {
    val m = manager
    val ch = channel
    if (m == null || ch == null || Build.VERSION.SDK_INT < Build.VERSION_CODES.Q) return result(0)
    try {
      m.requestGroupInfo(ch) { group -> result(group?.frequency ?: 0) }
    } catch (_: SecurityException) {
      result(0)
    }
  }

  /** Opens the group, or finds it already open. [done] gets null on success, otherwise a message. */
  fun create(credentials: WifiCredentials, done: (String?) -> Unit) {
    val m = manager
    val ch = channel
    if (closed) return done(CLOSED)
    if (m == null || ch == null) return done("Bu telefonda Wi-Fi Direct yok.")
    if (Build.VERSION.SDK_INT < Build.VERSION_CODES.Q) return done("Kendi Wi-Fi ağını kurmak için Android 10 veya üstü gerekir.")
    // Wi-Fi Direct lives on the Wi-Fi radio: with Wi-Fi off it only fails, with a number that says nothing.
    if (wifi?.isWifiEnabled == false) return done(WIFI_OFF_TO_START)
    // So does a missing permission ("hata 0"): say which one it is.
    missingWifiPermission(appContext)?.let { return done(it) }
    try {
      m.requestGroupInfo(ch) { group ->
        when {
          closed -> done(CLOSED)
          group != null && group.networkName == credentials.ssid -> done(null)
          group != null -> done("Başka bir uygulamanın Wi-Fi Direct grubu açık (${group.networkName}). Onu kapatıp yeniden dene.")
          else -> open(m, ch, credentials) { error ->
            // Closed while it was opening (sharing was stopped meanwhile): a group that opened all the same goes.
            if (error == null && closed) {
              runCatching { m.removeGroup(ch, null) }
              done(CLOSED)
            } else {
              done(error)
            }
          }
        }
      }
    } catch (_: SecurityException) {
      done("Wi-Fi izni verilmedi. Yakındaki cihazlar iznini (Android 12 ve altında konum) ver.")
    }
  }

  /**
   * Opens the group on the band the user picked. 2.4 GHz is seen by every computer. 5 GHz is faster: channel 36
   * first, which every country allows and every adapter scans ("auto" may pick upper channels some adapters do not
   * scan in their region), then any 5 GHz channel the system picks (a phone joined to a 5 GHz network elsewhere may
   * have to share that channel), and 2.4 GHz when neither opens.
   */
  @RequiresApi(Build.VERSION_CODES.Q)
  private fun open(m: WifiP2pManager, ch: WifiP2pManager.Channel, credentials: WifiCredentials, done: (String?) -> Unit) {
    val base = { WifiP2pConfig.Builder().setNetworkName(credentials.ssid).setPassphrase(credentials.passphrase).enablePersistentMode(false) }
    val tries = buildList {
      if (Controls.band5(appContext)) {
        add(base().setGroupOperatingFrequency(CHANNEL_36_MHZ).build())
        add(base().setGroupOperatingBand(WifiP2pConfig.GROUP_OWNER_BAND_5GHZ).build())
      }
      add(base().setGroupOperatingBand(WifiP2pConfig.GROUP_OWNER_BAND_2GHZ).build())
    }
    fun attempt(i: Int) {
      try {
        m.createGroup(ch, tries[i], object : WifiP2pManager.ActionListener {
          override fun onSuccess() = done(null)
          override fun onFailure(reason: Int) {
            if (i + 1 < tries.size && reason != WifiP2pManager.P2P_UNSUPPORTED) return attempt(i + 1)
            done(
              when (reason) {
                WifiP2pManager.BUSY -> "Wi-Fi Direct meşgul (başka bir uygulama kullanıyor olabilir). Kapatıp tekrar dene."
                WifiP2pManager.P2P_UNSUPPORTED -> "Bu telefon Wi-Fi Direct'i desteklemiyor."
                else -> "Wi-Fi ağı kurulamadı (hata $reason). Wi-Fi açık mı?" +
                  if (Build.VERSION.SDK_INT < 33) " Android 12 ve altında konum da açık olmalı." else ""
              }
            )
          }
        })
      } catch (_: SecurityException) {
        done("Wi-Fi izni verilmedi.")
      }
    }
    attempt(0)
  }

  /** True if the group of [credentials] is open right now; [result] is called with the answer. */
  fun isOpen(credentials: WifiCredentials, result: (Boolean) -> Unit) {
    val m = manager
    val ch = channel
    if (m == null || ch == null) return result(false)
    try {
      m.requestGroupInfo(ch) { group -> result(group != null && group.networkName == credentials.ssid) }
    } catch (_: SecurityException) {
      result(false)
    }
  }

  /**
   * Closes the group of [old] (if it is open) and opens one with [new] in its place. [done] gets null on success,
   * otherwise a message.
   */
  fun replace(old: WifiCredentials, new: WifiCredentials, done: (String?) -> Unit) {
    val m = manager
    val ch = channel
    if (m == null || ch == null) return done("Bu telefonda Wi-Fi Direct yok.")
    // Right after a group closes Wi-Fi Direct may still be busy for a moment: wait a little, and try once more.
    val reopen = {
      handler.postDelayed({
        create(new) { error ->
          if (error == null) done(null) else handler.postDelayed({ create(new, done) }, REOPEN_RETRY_MILLIS)
        }
      }, REOPEN_DELAY_MILLIS)
    }
    try {
      m.requestGroupInfo(ch) { group ->
        if (group != null && group.networkName == old.ssid) {
          m.removeGroup(ch, object : WifiP2pManager.ActionListener {
            override fun onSuccess() { reopen() }

            override fun onFailure(reason: Int) = done("Eski Wi-Fi ağı kapatılamadı (hata $reason).")
          })
        } else {
          reopen()
        }
      }
    } catch (_: SecurityException) {
      done("Wi-Fi izni verilmedi.")
    }
  }

  private val handler = android.os.Handler(Looper.getMainLooper())

  /** Set by [remove]: from then on this object opens no group any more. */
  @Volatile
  private var closed = false

  /**
   * Closes the group, but only if it is ours. Nothing of this object opens one afterwards: not a reopening that
   * still waits its turn (see [replace]), nor an opening under way, whose group goes when it arrives (see
   * [create]). Sharing that was stopped must not leave a network behind, or get one a few seconds later.
   */
  fun remove(credentials: WifiCredentials) {
    closed = true
    handler.removeCallbacksAndMessages(null)
    val m = manager
    val ch = channel
    if (m == null || ch == null) return
    try {
      m.requestGroupInfo(ch) { group ->
        if (group != null && group.networkName == credentials.ssid) m.removeGroup(ch, null)
      }
    } catch (_: SecurityException) {
      // nothing to remove without the permission
    }
  }

  private companion object {
    const val CLOSED = "Paylaşım durduruldu."
    const val REOPEN_DELAY_MILLIS = 1_000L
    const val REOPEN_RETRY_MILLIS = 3_000L
    /** Channel 36, the lowest 5 GHz channel: allowed indoors everywhere, scanned by every adapter. */
    const val CHANNEL_36_MHZ = 5180
  }
}

/**
 * Why the app may not open its Wi-Fi network, or null when it may: Android wants a permission for Wi-Fi Direct,
 * "nearby devices" from Android 13 on and the precise location before that.
 */
internal fun missingWifiPermission(context: Context): String? {
  val nearby = Build.VERSION.SDK_INT >= 33
  val permission = if (nearby) Manifest.permission.NEARBY_WIFI_DEVICES else Manifest.permission.ACCESS_FINE_LOCATION
  if (ContextCompat.checkSelfPermission(context, permission) == PackageManager.PERMISSION_GRANTED) return null
  val where = "Ayarlar → Uygulamalar → AleShare → İzinler bölümünden ver, sonra yeniden başlat."
  return if (nearby) "Wi-Fi ağını açmak için \"Yakındaki cihazlar\" izni gerekli. $where"
  else "Wi-Fi ağını açmak için konum izni gerekli (\"Tam konum\" seçili olmalı). $where"
}
