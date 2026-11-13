package dev.hotspot.app

import android.content.Context

/**
 * Guests: devices added for a while (a friend's phone, with the WireGuard app), for a time and/or an amount of
 * data. When either runs out the device is removed, keys and all; it has to be added again to come back.
 */
object Guests {
  /** [until] in epoch milliseconds and [limitMb] in MiB, each 0 for none; [used] bytes so far. */
  data class Guest(val name: String, val until: Long, val limitMb: Long, val used: Long)

  fun add(context: Context, name: String, until: Long, limitMb: Long) = save(context, Guest(name, until, limitMb, 0))

  fun get(context: Context, name: String): Guest? =
    prefs(context).getString(name, null)?.split(';')?.mapNotNull { it.toLongOrNull() }?.takeIf { it.size == 3 }?.let {
      Guest(name, it[0], it[1], it[2])
    }

  fun forget(context: Context, name: String) = prefs(context).edit().remove(name).apply()

  /** Adds what the guests used (see DataUsage.record). */
  fun record(context: Context, used: Map<String, Long>) {
    for ((name, bytes) in used) get(context, name)?.let { save(context, it.copy(used = it.used + bytes)) }
  }

  /** The guests whose time or data is over. */
  fun over(context: Context, now: Long = System.currentTimeMillis()): List<String> =
    prefs(context).all.keys.mapNotNull { get(context, it) }.filter { guestOver(it, now) }.map { it.name }

  /** "Misafir · 45 dk · 120 MB / 500 MB" for a guest, null for a device of the household. */
  fun note(context: Context, name: String, now: Long = System.currentTimeMillis()): String? =
    get(context, name)?.let { guestNote(it, now) }

  private fun save(context: Context, g: Guest) = prefs(context).edit().putString(g.name, "${g.until};${g.limitMb};${g.used}").apply()

  private fun prefs(context: Context) = context.getSharedPreferences("guests", Context.MODE_PRIVATE)
}

internal fun guestOver(g: Guests.Guest, now: Long): Boolean =
  (g.until > 0 && now >= g.until) || (g.limitMb > 0 && g.used >= g.limitMb * 1024 * 1024)

internal fun guestNote(g: Guests.Guest, now: Long): String {
  val left = (g.until - now).coerceAtLeast(0) / 60_000
  val time = when {
    g.until <= 0 -> null
    left >= 120 -> "${left / 60} saat kaldı"
    left >= 60 -> "1 saat ${left - 60} dk kaldı"
    else -> "$left dk kaldı"
  }
  val data = if (g.limitMb > 0) "${humanBytes(g.used)} / ${limitLabel(g.limitMb)}" else null
  return listOfNotNull("Misafir", time, data).joinToString(" · ")
}
