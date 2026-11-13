package dev.hotspot.app

import android.content.Context
import android.os.SystemClock
import dev.hotspot.wgcore.Core
import java.time.LocalDate

/**
 * The mobile data that sharing used today, in all and per device, kept across restarts; a new day starts at local
 * midnight. The total is what the gateway sent and received over the cellular network. A device's share is what its
 * flows moved over it, counted at the same place (Core.peerTraffic), so the shares add up to the total; what a device
 * exchanges with the phone itself (a speed test, a release) costs no mobile data and is in neither. (Up to 1.0.22 a
 * share was what the device's tunnel carried, the phone's own services included: far more than the total.)
 */
object DataUsage {
  data class Today(val total: Long = 0, val devices: Map<String, Long> = emptyMap())

  private const val PREFS = "usage"
  private const val KEY_DAY = "day"
  private const val KEY_TOTAL = "total"
  private const val DEVICE_PREFIX = "device:"
  private const val KEY_LIMIT_MB = "limit_mb"
  private const val KEY_WARNED_DAY = "warned_day"
  private const val KEY_STOPPED_DAY = "stopped_day"
  private const val KEY_HISTORY = "history"
  private const val HISTORY_DAYS = 62L
  private const val SAVE_EVERY_MILLIS = 30_000L

  private var loaded = false
  private var day = ""
  private var today = Today()
  private var savedAt = 0L
  private var dirty = false

  /** The core's counters at the last reading; they start from zero again with every start of sharing. */
  private var lastTotal = 0L
  private var lastDevices = emptyMap<String, Long>()

  @Synchronized
  fun today(context: Context): Today {
    load(context)
    rollOver(context)
    return today
  }

  /** Sharing (re)started: the core's counters begin at zero. */
  @Synchronized
  fun started() {
    lastTotal = 0
    lastDevices = emptyMap()
  }

  /** Takes a reading of the running core and adds what was used since the last one; returns that, per device. */
  @Synchronized
  fun record(context: Context, core: Core): Map<String, Long> {
    load(context)
    rollOver(context)
    val stats = core.stats()
    val total = stats.bytesUp + stats.bytesDown
    val devices = parseTraffic(core.peerTraffic())
    val added = counterDelta(total, lastTotal)
    val grown = today.devices.toMutableMap()
    val deltas = mutableMapOf<String, Long>()
    devices.forEach { (name, bytes) ->
      val d = counterDelta(bytes, lastDevices[name] ?: 0)
      if (d > 0) {
        grown[name] = (grown[name] ?: 0) + d
        deltas[name] = d
      }
    }
    lastTotal = total
    lastDevices = devices
    if (added > 0 || grown != today.devices) {
      today = Today(today.total + added, grown)
      dirty = true
    }
    if (dirty && SystemClock.elapsedRealtime() - savedAt >= SAVE_EVERY_MILLIS) save(context)
    return deltas
  }

  /** What sharing used on each of the last days that are kept (about two months), today's so far included. */
  @Synchronized
  fun history(context: Context): Map<LocalDate, Long> {
    load(context)
    rollOver(context)
    return parseHistory(prefs(context).getString(KEY_HISTORY, null)) + (LocalDate.parse(day) to today.total)
  }

  /** Writes what is only in memory: when sharing stops, and now and then while it runs. */
  @Synchronized
  fun flush(context: Context) {
    if (loaded && dirty) save(context)
  }

  /** The daily limit in MiB; 0 means none. */
  fun limitMb(context: Context): Long = prefs(context).getLong(KEY_LIMIT_MB, 0)

  fun setLimitMb(context: Context, mb: Long) {
    prefs(context).edit().putLong(KEY_LIMIT_MB, mb).apply()
  }

  /**
   * What the limit asks for now, each at most once a day: a warning at 90 %, and stopping sharing when it is
   * reached. A user who starts sharing again after that is not stopped again on the same day.
   */
  @Synchronized
  fun limitAction(context: Context): LimitAction {
    val limit = limitMb(context) * 1024 * 1024
    val p = prefs(context)
    val action = limitAction(today.total, limit, warned = p.getString(KEY_WARNED_DAY, null) == day, stopped = p.getString(KEY_STOPPED_DAY, null) == day)
    when (action) {
      LimitAction.Warn -> p.edit().putString(KEY_WARNED_DAY, day).apply()
      LimitAction.Stop -> p.edit().putString(KEY_WARNED_DAY, day).putString(KEY_STOPPED_DAY, day).apply()
      LimitAction.None -> {}
    }
    return action
  }

  private fun load(context: Context) {
    if (loaded) return
    val p = prefs(context)
    day = p.getString(KEY_DAY, null) ?: LocalDate.now().toString()
    val devices = p.all.filterKeys { it.startsWith(DEVICE_PREFIX) }
      .mapNotNull { (k, v) -> (v as? Long)?.let { k.removePrefix(DEVICE_PREFIX) to it } }
      .toMap()
    today = Today(p.getLong(KEY_TOTAL, 0), devices)
    loaded = true
  }

  private fun rollOver(context: Context) {
    val now = LocalDate.now().toString()
    if (now == day) return
    // The day that ended goes into the history, for the monthly plan and its chart.
    val p = prefs(context)
    val kept = parseHistory(p.getString(KEY_HISTORY, null)) + (LocalDate.parse(day) to today.total)
    p.edit().putString(KEY_HISTORY, historyText(kept, LocalDate.parse(now).minusDays(HISTORY_DAYS))).apply()
    day = now
    today = Today()
    save(context)
  }

  private fun save(context: Context) {
    val e = prefs(context).edit()
    prefs(context).all.keys.filter { it.startsWith(DEVICE_PREFIX) }.forEach { e.remove(it) }
    e.putString(KEY_DAY, day).putLong(KEY_TOTAL, today.total)
    today.devices.forEach { (name, bytes) -> e.putLong(DEVICE_PREFIX + name, bytes) }
    e.apply()
    savedAt = SystemClock.elapsedRealtime()
    dirty = false
  }

  private fun prefs(context: Context) = context.getSharedPreferences(PREFS, Context.MODE_PRIVATE)
}

enum class LimitAction { None, Warn, Stop }

/** What a counter grew by since [last]; one that went down was started anew (sharing restarted, devices changed). */
internal fun counterDelta(now: Long, last: Long): Long = if (now >= last) now - last else now

/** The limit's verdict for [used] bytes of [limit] (0: no limit), given what was already done today. */
internal fun limitAction(used: Long, limit: Long, warned: Boolean, stopped: Boolean): LimitAction =
  when {
    limit <= 0 -> LimitAction.None
    used >= limit && !stopped -> LimitAction.Stop
    used >= limit * 9 / 10 && !warned && !stopped -> LimitAction.Warn
    else -> LimitAction.None
  }

/** A limit as the user picks it: whole gigabytes (of 1024 MiB, as [humanBytes] counts) or megabytes. */
fun limitLabel(mb: Long): String = if (mb >= 1024 && mb % 1024 == 0L) "${mb / 1024} GB" else "$mb MB"

/** The kept days, "2026-10-31=123456;…"; anything unreadable is left out. */
internal fun parseHistory(text: String?): Map<LocalDate, Long> =
  text.orEmpty().split(';').mapNotNull { entry ->
    val (d, b) = entry.split('=').takeIf { it.size == 2 } ?: return@mapNotNull null
    runCatching { LocalDate.parse(d) }.getOrNull()?.let { date -> b.toLongOrNull()?.let { date to it } }
  }.toMap()

/** The days from [after] on (exclusive), in order, written as [parseHistory] reads them. */
internal fun historyText(days: Map<LocalDate, Long>, after: LocalDate): String =
  days.filterKeys { it.isAfter(after) }.toSortedMap().entries.joinToString(";") { "${it.key}=${it.value}" }

/** The core's "name<TAB>bytes" lines (see Core.peerTraffic). */
internal fun parseTraffic(text: String): Map<String, Long> =
  text.lines().mapNotNull { line ->
    val tab = line.lastIndexOf('\t')
    if (tab <= 0) null else line.substring(tab + 1).toLongOrNull()?.let { line.substring(0, tab) to it }
  }.toMap()
