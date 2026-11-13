package dev.hotspot.app

import android.content.Context
import android.os.BatteryManager
import android.os.Build
import android.telephony.CellSignalStrengthLte
import android.telephony.CellSignalStrengthNr
import android.telephony.TelephonyManager
import dev.hotspot.wgcore.Core
import org.json.JSONArray
import org.json.JSONObject
import java.time.LocalDate
import java.time.LocalDateTime

/**
 * What the user decides about the shared traffic, kept in the app's settings and handed to the core: blocking of
 * ads and trackers, saving data (no big automatic downloads), the priority of calls and games, the Wi-Fi band, the
 * battery and heat guard, stopping when nobody is connected, and per device a daily limit, a speed limit, quiet
 * hours and a pause. The same switches can be changed from a paired computer (see [applyControl]).
 */
object Controls {
  private const val KEY_ADS = "block_ads"
  private const val KEY_SAVER = "save_data"
  private const val KEY_PRIORITY = "priority"
  private const val KEY_BAND = "band_5ghz"
  private const val KEY_GUARD = "battery_guard"
  private const val KEY_IDLE_STOP = "idle_stop_min"
  private const val DEVICE_LIMIT = "device_limit_mb:"
  private const val DEVICE_SPEED = "device_speed_kbps:"
  private const val DEVICE_QUIET = "device_quiet:" // "from-to", minutes of the day
  private const val DEVICE_QUIET_SKIP = "device_quiet_skip:" // the day whose quiet hours the user lifted
  // "manual"; the day a daily limit ran out; or "free:" and the day the user resumed the device despite its limit
  private const val DEVICE_PAUSED = "device_paused:"

  private fun prefs(context: Context) = context.getSharedPreferences("settings", Context.MODE_PRIVATE)

  /** Ads and trackers are blocked unless the user turned it off. */
  fun blockAds(context: Context) = prefs(context).getBoolean(KEY_ADS, true)
  fun setBlockAds(context: Context, on: Boolean) = prefs(context).edit().putBoolean(KEY_ADS, on).apply()

  /** Big automatic downloads (system and game updates) are let through unless the user turned saving on. */
  fun saveData(context: Context) = prefs(context).getBoolean(KEY_SAVER, false)
  fun setSaveData(context: Context, on: Boolean) = prefs(context).edit().putBoolean(KEY_SAVER, on).apply()

  /** Calls and games come first unless the user turned it off. */
  fun priority(context: Context) = prefs(context).getBoolean(KEY_PRIORITY, true)
  fun setPriority(context: Context, on: Boolean) = prefs(context).edit().putBoolean(KEY_PRIORITY, on).apply()

  /** The app's own network on 5 GHz (faster) instead of 2.4 GHz (seen by every computer, the default). */
  fun band5(context: Context) = prefs(context).getBoolean(KEY_BAND, false)
  fun setBand5(context: Context, on: Boolean) = prefs(context).edit().putBoolean(KEY_BAND, on).apply()

  /** The phone slows sharing down when it gets hot and stops it when its battery is nearly flat, unless turned off. */
  fun guard(context: Context) = prefs(context).getBoolean(KEY_GUARD, true)
  fun setGuard(context: Context, on: Boolean) = prefs(context).edit().putBoolean(KEY_GUARD, on).apply()

  /** Minutes without any connected device after which sharing stops by itself; 0 means never. */
  fun idleStopMin(context: Context) = prefs(context).getLong(KEY_IDLE_STOP, 0)
  fun setIdleStopMin(context: Context, minutes: Long) = prefs(context).edit().putLong(KEY_IDLE_STOP, minutes).apply()

  /** A device's daily limit in MiB; 0 means none. */
  fun deviceLimitMb(context: Context, name: String) = prefs(context).getLong(DEVICE_LIMIT + name, 0)
  fun setDeviceLimitMb(context: Context, name: String, mb: Long) = prefs(context).edit().putLong(DEVICE_LIMIT + name, mb).apply()

  /** A device's speed limit in kilobits per second, each way; 0 means none. */
  fun deviceSpeedKbps(context: Context, name: String) = prefs(context).getLong(DEVICE_SPEED + name, 0)
  fun setDeviceSpeedKbps(context: Context, core: Core?, name: String, kbps: Long) {
    prefs(context).edit().putLong(DEVICE_SPEED + name, kbps).apply()
    runCatching { core?.setSpeedLimit(name, kbps) }
  }

  /** The hours of the day a device has no internet (a child's tablet at night), or null. */
  fun quietHours(context: Context, name: String): QuietHours? = QuietHours.parse(prefs(context).getString(DEVICE_QUIET + name, null))
  fun setQuietHours(context: Context, core: Core?, name: String, quiet: QuietHours?) {
    prefs(context).edit().apply {
      if (quiet == null) remove(DEVICE_QUIET + name) else putString(DEVICE_QUIET + name, quiet.toString())
      remove(DEVICE_QUIET_SKIP + name)
    }.apply()
    core?.let { follow(context, it, name) }
  }

  /**
   * Why a device is paused: [Pause.Manual] by the user, [Pause.Limit] until midnight, [Pause.Quiet] during its
   * quiet hours, or not at all.
   */
  enum class Pause { None, Manual, Limit, Quiet }

  fun pause(context: Context, name: String, now: LocalDateTime = LocalDateTime.now()): Pause {
    val p = prefs(context)
    return pauseOf(p.getString(DEVICE_PAUSED + name, null), quietHours(context, name), p.getString(DEVICE_QUIET_SKIP + name, null), now)
  }

  /**
   * Pauses a device by hand, or resumes it: its own pause, a used-up limit (for the rest of the day) and quiet hours
   * under way (until they end).
   */
  fun setPaused(context: Context, core: Core?, name: String, paused: Boolean) {
    val now = LocalDateTime.now()
    val p = prefs(context)
    p.edit().apply {
      if (paused) {
        putString(DEVICE_PAUSED + name, "manual")
      } else {
        if (p.getString(DEVICE_PAUSED + name, null) == now.toLocalDate().toString()) {
          putString(DEVICE_PAUSED + name, FREE + now.toLocalDate())
        } else {
          remove(DEVICE_PAUSED + name)
        }
        quietHours(context, name)?.takeIf { it.contains(now) }?.let { putString(DEVICE_QUIET_SKIP + name, it.startOf(now).toString()) }
      }
    }.apply()
    core?.let { follow(context, it, name) }
  }

  /** The devices whose pause the core holds now, as last handed over; see [follow]. */
  private val applied = mutableMapOf<String, Boolean>()

  /** Hands the core whether [name] is paused now, when that changed. */
  @Synchronized
  private fun follow(context: Context, core: Core, name: String) {
    val paused = pause(context, name) != Pause.None
    if (applied[name] != paused) {
      runCatching { core.setPaused(name, paused) }
      applied[name] = paused
    }
  }

  /** Hands the core every switch, pause and speed limit, at the start of sharing. */
  fun applyAll(context: Context, core: Core, devices: List<String>) {
    core.setBlockAds(blockAds(context))
    core.setSaveData(saveData(context))
    core.setPriority(priority(context))
    core.setSpeedCap(0)
    synchronized(this) { applied.clear() }
    for (name in devices) {
      follow(context, core, name)
      runCatching { core.setSpeedLimit(name, deviceSpeedKbps(context, name)) }
    }
  }

  /**
   * Pauses the devices whose daily limit ran out (until midnight) or whose quiet hours began, and resumes those whose
   * day or quiet hours are over. Returns the names that were just paused for their limit, to tell the user.
   */
  fun enforceLimits(context: Context, core: Core, devices: List<String>, usedToday: Map<String, Long>): List<String> {
    val stopped = mutableListOf<String>()
    val p = prefs(context)
    val today = LocalDate.now().toString()
    for (name in devices) {
      val stored = p.getString(DEVICE_PAUSED + name, null)
      // Yesterday's used-up (or lifted) limit: the device starts the day afresh.
      if (stored != null && stored != "manual" && stored != today && stored != FREE + today) {
        p.edit().remove(DEVICE_PAUSED + name).apply()
      }
      val limit = deviceLimitMb(context, name) * 1024 * 1024
      if (p.getString(DEVICE_PAUSED + name, null) == null && overLimit(usedToday[name] ?: 0, limit)) {
        p.edit().putString(DEVICE_PAUSED + name, today).apply()
        stopped += name
      }
      follow(context, core, name)
    }
    return stopped
  }

  private const val FREE = "free:"

  /**
   * Applies one change a paired computer asked for (a JSON object). Only the switches the app shows are taken, each
   * with a value of the right kind; anything else is ignored. Returns whether something changed.
   */
  fun applyControl(context: Context, core: Core, json: String): Boolean {
    val o = runCatching { JSONObject(json) }.getOrNull() ?: return false
    var changed = false
    fun flag(key: String, set: (Boolean) -> Unit) {
      if (o.has(key)) (o.opt(key) as? Boolean)?.let { set(it); changed = true }
    }
    flag("split") { setSplitPreference(context, it); core.setSplitTLS(it) }
    flag("ads") { setBlockAds(context, it); core.setBlockAds(it) }
    flag("saver") { setSaveData(context, it); core.setSaveData(it) }
    flag("priority") { setPriority(context, it); core.setPriority(it) }
    flag("guard") { setGuard(context, it) } // the service applies it on its next look
    if (o.optBoolean("speedtest")) {
      SpeedTest.start(context)
      changed = true
    }
    if (o.has("limit_mb")) (o.opt("limit_mb") as? Number)?.toLong()?.takeIf { it in 0..1_048_576 }?.let {
      DataUsage.setLimitMb(context, it)
      changed = true
    }
    for ((key, paused) in listOf("pause" to true, "resume" to false)) {
      (o.opt(key) as? String)?.let { name ->
        if (core.peerNames().lines().contains(name)) {
          setPaused(context, core, name, paused)
          changed = true
        }
      }
    }
    return changed
  }

  /**
   * What paired computers are told about the phone: battery, mobile network and signal, data used and the speed now,
   * the switches, and the devices with their use today. [downBps] and [upBps] are bytes per second.
   */
  fun status(context: Context, core: Core, downBps: Long, upBps: Long, plan: Plan.Status?): String {
    val usage = DataUsage.today(context)
    val devices = JSONArray()
    for (name in core.peerNames().lines().filter { it.isNotBlank() }) {
      devices.put(
        JSONObject()
          .put("name", name)
          .put("today", usage.devices[name] ?: 0)
          .put("limit_mb", deviceLimitMb(context, name))
          .put("speed_kbps", deviceSpeedKbps(context, name))
          .put("quiet", quietHours(context, name)?.label() ?: "")
          .put("guest", Guests.note(context, name) ?: "")
          .put("paused", pause(context, name).name.lowercase())
      )
    }
    val speed = SpeedTest.state.value
    val speedJson = JSONObject().put("running", speed.running).put("error", speed.error ?: "")
    speed.last?.let {
      speedJson.put("down_bps", it.downBps).put("up_bps", it.upBps).put("ping_ms", it.pingMs).put("bytes", it.bytes).put("at", it.at)
    }
    val battery = context.getSystemService(BatteryManager::class.java)
    val (network, signal) = mobileSignal(context)
    return JSONObject()
      .put("version", runCatching { context.packageManager.getPackageInfo(context.packageName, 0).versionName }.getOrNull() ?: "")
      .put("battery", battery?.getIntProperty(BatteryManager.BATTERY_PROPERTY_CAPACITY) ?: -1)
      // Plugged in, from the battery's last broadcast: it changes the moment the cable does. ("Is it charging"
      // lags behind, and says no while plugged in when the phone holds its charge back to spare the battery.)
      .put("charging", Guard.read(context).charging)
      .put("network", network)
      .put("signal", signal)
      .put("today", usage.total)
      .put("limit_mb", DataUsage.limitMb(context))
      .put("down_bps", downBps)
      .put("up_bps", upBps)
      .put("blocked", core.stats().dnsBlocked)
      .put("split", splitPreference(context))
      .put("ads", blockAds(context))
      .put("saver", saveData(context))
      .put("priority", priority(context))
      .put("band", if (band5(context)) "5" else "2.4")
      .put("guard", guard(context))
      .put("hot", Guard.hot)
      // What Android says of the two permissions the app asks for in its screens.
      .put("battery_free", context.getSystemService(android.os.PowerManager::class.java)?.isIgnoringBatteryOptimizations(context.packageName) ?: false)
      .put("usage_access", Plan.hasUsageAccess(context))
      .put("devices", devices)
      .put("speedtest", speedJson)
      .apply {
        plan?.let {
          put("plan", JSONObject().put("limit_mb", it.limitMb).put("used", it.used).put("days_left", it.daysLeft).put("per_day", it.perDay).put("whole_phone", it.wholePhone))
        }
      }
      .toString()
  }

  /** The mobile network's generation ("5G", "4G"…) and its signal from 0 (none) to 4 (best); "" and -1 when unknown. */
  private fun mobileSignal(context: Context): Pair<String, Int> {
    if (Build.VERSION.SDK_INT < 29) return "" to -1
    val strength = runCatching { context.getSystemService(TelephonyManager::class.java)?.signalStrength }.getOrNull() ?: return "" to -1
    val cells = strength.cellSignalStrengths
    val network = when {
      cells.any { it is CellSignalStrengthNr } -> "5G"
      cells.any { it is CellSignalStrengthLte } -> "4G"
      cells.isNotEmpty() -> "3G"
      else -> ""
    }
    return network to strength.level
  }
}

/** Whether a device used up its daily limit (0: no limit). */
internal fun overLimit(used: Long, limit: Long): Boolean = limit > 0 && used >= limit

/**
 * Why a device is paused, from what is stored about it: [stored] is its pause mark (see Controls), [skip] the day
 * whose quiet hours the user lifted.
 */
internal fun pauseOf(stored: String?, quiet: QuietHours?, skip: String?, now: LocalDateTime): Controls.Pause =
  when {
    stored == "manual" -> Controls.Pause.Manual
    stored == now.toLocalDate().toString() -> Controls.Pause.Limit
    quiet != null && quiet.contains(now) && quiet.startOf(now).toString() != skip -> Controls.Pause.Quiet
    else -> Controls.Pause.None // no mark, a lifted limit, or a mark of an earlier day
  }

/** Hours without internet, [from] to [to] in minutes of the day; past midnight when [to] comes before [from]. */
data class QuietHours(val from: Int, val to: Int) {
  fun contains(now: LocalDateTime): Boolean {
    val m = now.hour * 60 + now.minute
    return when {
      from == to -> false
      from < to -> m in from until to
      else -> m >= from || m < to
    }
  }

  /** The day the quiet hours under way at [now] began: yesterday after midnight, when they started the evening before. */
  fun startOf(now: LocalDateTime): LocalDate =
    if (from > to && now.hour * 60 + now.minute < to) now.toLocalDate().minusDays(1) else now.toLocalDate()

  fun label(): String = "${clock(from)}–${clock(to)}"

  override fun toString() = "$from-$to"

  companion object {
    fun parse(text: String?): QuietHours? {
      val parts = text?.split('-')?.mapNotNull { it.toIntOrNull() } ?: return null
      return if (parts.size == 2 && parts.all { it in 0 until 24 * 60 }) QuietHours(parts[0], parts[1]) else null
    }

    fun clock(minutes: Int) = "%02d:%02d".format(minutes / 60, minutes % 60)

    /** "07:30'da": the time with the ending Turkish gives it. */
    fun clockAt(minutes: Int) = Tr.atClock(minutes)
  }
}
