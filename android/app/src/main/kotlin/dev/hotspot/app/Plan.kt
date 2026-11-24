package dev.hotspot.app

import android.Manifest
import android.app.AppOpsManager
import android.content.pm.PackageManager
import android.app.usage.NetworkStatsManager
import android.content.Context
import android.net.ConnectivityManager
import android.os.Build
import android.os.Process
import java.time.LocalDate
import java.time.ZoneId
import java.time.temporal.ChronoUnit

/**
 * The monthly mobile plan: how big it is and the day of the month it renews. What counts against it is the whole
 * phone's mobile data when the user allowed the app to read it (usage access), and otherwise what sharing used.
 * The reading of the phone's data is a call into the system: off the main thread.
 */
object Plan {
  private const val KEY_MB = "plan_mb"
  private const val KEY_DAY = "plan_renew_day"
  private const val KEY_WARNED = "plan_warned" // the period's first day and the share told about, "2026-11-01:90"

  /** The plan in MiB; 0 means none was given. */
  fun limitMb(context: Context) = prefs(context).getLong(KEY_MB, 0)

  /** The day of the month the plan starts over, 1 to 31 (the month's last day in shorter months). */
  fun renewDay(context: Context) = prefs(context).getInt(KEY_DAY, 1)

  fun set(context: Context, mb: Long, renewDay: Int) =
    prefs(context).edit().putLong(KEY_MB, mb).putInt(KEY_DAY, renewDay.coerceIn(1, 31)).apply()

  data class Status(
    val limitMb: Long,
    val used: Long,
    val start: LocalDate,
    val next: LocalDate,
    val daysLeft: Int,
    val perDay: Long, // what can be used each day left, today included, for the plan to last
    val wholePhone: Boolean, // the phone's own data counts too, not only sharing
  )

  /** Where the plan stands today; null when there is no plan. */
  fun status(context: Context): Status? {
    val limitMb = limitMb(context).takeIf { it > 0 } ?: return null
    val today = LocalDate.now()
    val start = periodStart(today, renewDay(context))
    val next = nextRenewal(today, renewDay(context))
    val phone = phoneBytes(context, start, today.plusDays(1))
    val used = phone ?: DataUsage.history(context).filterKeys { !it.isBefore(start) }.values.sum()
    val daysLeft = ChronoUnit.DAYS.between(today, next).toInt()
    return Status(limitMb, used, start, next, daysLeft, perDayAllowance(limitMb * 1024 * 1024 - used, daysLeft), phone != null)
  }

  /** One day of the chart: what sharing used, and what the whole phone did when that can be read. */
  data class Day(val date: LocalDate, val shared: Long, val phone: Long?)

  /** The last [count] days, oldest first, today last. */
  fun days(context: Context, count: Int = 30): List<Day> {
    val shared = DataUsage.history(context)
    val today = LocalDate.now()
    val whole = hasUsageAccess(context)
    return (count - 1 downTo 0).map { back ->
      val date = today.minusDays(back.toLong())
      Day(date, shared[date] ?: 0, if (whole) phoneBytes(context, date, date.plusDays(1)) else null)
    }
  }

  /**
   * The share of the plan to tell the user about now, once a period each: 90 when nine tenths are used, 100 when
   * all of it is; null otherwise.
   */
  fun alert(context: Context, s: Status): Int? {
    val level = when {
      s.used >= s.limitMb * 1024 * 1024 -> 100
      s.used >= s.limitMb * 1024 * 1024 * 9 / 10 -> 90
      else -> return null
    }
    val told = prefs(context).getString(KEY_WARNED, null)?.split(':')
    if (told?.getOrNull(0) == s.start.toString() && (told.getOrNull(1)?.toIntOrNull() ?: 0) >= level) return null
    prefs(context).edit().putString(KEY_WARNED, "${s.start}:$level").apply()
    return level
  }

  /** Whether the user let the app read the phone's data use (Settings → Apps → Special access → Usage access). */
  fun hasUsageAccess(context: Context): Boolean {
    if (Build.VERSION.SDK_INT < 29) return false
    val ops = context.getSystemService(AppOpsManager::class.java) ?: return false
    // "Default" means the permission decides (as on some phones after the switch in the settings): ask it then.
    return when (ops.unsafeCheckOpNoThrow(AppOpsManager.OPSTR_GET_USAGE_STATS, Process.myUid(), context.packageName)) {
      AppOpsManager.MODE_ALLOWED -> true
      AppOpsManager.MODE_DEFAULT -> context.checkSelfPermission(Manifest.permission.PACKAGE_USAGE_STATS) == PackageManager.PERMISSION_GRANTED
      else -> false
    }
  }

  /** The whole phone's mobile data from [from] to [until] (exclusive), sharing included; null when it cannot be read. */
  private fun phoneBytes(context: Context, from: LocalDate, until: LocalDate): Long? {
    if (!hasUsageAccess(context)) return null
    val zone = ZoneId.systemDefault()
    return runCatching {
      val stats = context.getSystemService(NetworkStatsManager::class.java)
      @Suppress("DEPRECATION")
      val b = stats.querySummaryForDevice(
        ConnectivityManager.TYPE_MOBILE, null,
        from.atStartOfDay(zone).toInstant().toEpochMilli(), until.atStartOfDay(zone).toInstant().toEpochMilli(),
      )
      b.rxBytes + b.txBytes
    }.getOrNull()
  }

  private fun prefs(context: Context) = context.getSharedPreferences("settings", Context.MODE_PRIVATE)
}

/** The first day of the plan's period that [today] is in. */
internal fun periodStart(today: LocalDate, renewDay: Int): LocalDate {
  val here = today.withDayOfMonth(minOf(renewDay, today.lengthOfMonth()))
  if (!today.isBefore(here)) return here
  val before = today.minusMonths(1)
  return before.withDayOfMonth(minOf(renewDay, before.lengthOfMonth()))
}

/** The day the plan starts over next. */
internal fun nextRenewal(today: LocalDate, renewDay: Int): LocalDate {
  val after = periodStart(today, renewDay).plusMonths(1)
  return after.withDayOfMonth(minOf(renewDay, after.lengthOfMonth()))
}

/** What each of [daysLeft] days (today included) may use for [remaining] bytes to last. */
internal fun perDayAllowance(remaining: Long, daysLeft: Int): Long = remaining.coerceAtLeast(0) / daysLeft.coerceAtLeast(1)
