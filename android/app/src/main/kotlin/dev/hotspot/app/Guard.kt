package dev.hotspot.app

import android.content.Context
import android.content.Intent
import android.content.IntentFilter
import android.os.BatteryManager
import android.os.Build
import android.os.PowerManager

/**
 * Keeps a sharing phone from overheating or running flat: hot, it slows the shared traffic down until it cools;
 * nearly flat and not charging, it warns, then stops sharing. Looked at by the sharing service every few seconds.
 */
object Guard {
  /** Whether the phone counts as hot now (and sharing is slowed down). */
  @Volatile
  var hot = false
    private set

  // Told already, and stopped already, since the phone was last charged: started again, sharing goes on.
  private var batteryWarned = false
  private var batteryStopped = false

  /** The phone's battery and heat now: charge in percent, charging, battery temperature in tenths of a degree, and the system's thermal status. */
  data class Reading(val level: Int, val charging: Boolean, val tempTenths: Int, val thermal: Int)

  fun read(context: Context): Reading {
    // The battery's last broadcast, kept by the system: no receiver is registered.
    val battery = context.registerReceiver(null, IntentFilter(Intent.ACTION_BATTERY_CHANGED))
    val level = battery?.let {
      val scale = it.getIntExtra(BatteryManager.EXTRA_SCALE, 100).coerceAtLeast(1)
      it.getIntExtra(BatteryManager.EXTRA_LEVEL, -1).takeIf { l -> l >= 0 }?.let { l -> l * 100 / scale }
    } ?: -1
    val charging = (battery?.getIntExtra(BatteryManager.EXTRA_PLUGGED, 0) ?: 0) != 0
    val temp = battery?.getIntExtra(BatteryManager.EXTRA_TEMPERATURE, 0) ?: 0
    val thermal = if (Build.VERSION.SDK_INT >= 29) context.getSystemService(PowerManager::class.java)?.currentThermalStatus ?: 0 else 0
    return Reading(level, charging, temp, thermal)
  }

  enum class Change { None, GotHot, CooledDown }

  /** Notes [r] and says whether the phone just got hot or cooled down. */
  fun heat(r: Reading, on: Boolean): Change {
    val now = on && hotNow(r.tempTenths, r.thermal, hot)
    if (now == hot) return Change.None
    hot = now
    return if (now) Change.GotHot else Change.CooledDown
  }

  /** What the battery asks for now; the warning and the stop come once each until the phone is charged again. */
  fun battery(r: Reading, on: Boolean): BatteryAction {
    if (r.charging || r.level > BATTERY_REARM) {
      batteryWarned = false
      batteryStopped = false
    }
    if (!on) return BatteryAction.None
    val action = batteryAction(r.level, r.charging, batteryWarned, batteryStopped)
    if (action != BatteryAction.None) batteryWarned = true
    if (action == BatteryAction.Stop) batteryStopped = true
    return action
  }

  /** Sharing stopped: the next start is not slowed down before it gets hot again. */
  fun reset() {
    hot = false
  }

  /** The speed all of sharing is held to while the phone is hot, kilobits per second. */
  const val HOT_CAP_KBPS = 10_000L
  const val BATTERY_WARN = 15
  const val BATTERY_STOP = 5
  private const val BATTERY_REARM = 20
}

enum class BatteryAction { None, Warn, Stop }

/** Hot from 45 °C of the battery or a severe thermal status of the system; cool again under 42 °C. */
internal fun hotNow(tempTenths: Int, thermal: Int, wasHot: Boolean): Boolean =
  thermal >= THERMAL_SEVERE || tempTenths >= if (wasHot) 420 else 450

internal fun batteryAction(level: Int, charging: Boolean, warned: Boolean, stopped: Boolean): BatteryAction =
  when {
    charging || level < 0 -> BatteryAction.None
    level <= Guard.BATTERY_STOP && !stopped -> BatteryAction.Stop
    level <= Guard.BATTERY_WARN && !warned -> BatteryAction.Warn
    else -> BatteryAction.None
  }

/** Whether sharing has gone [minutes] without any connected device (0: never stops). */
internal fun idleOver(nowMillis: Long, lastBusyMillis: Long, minutes: Long): Boolean =
  minutes > 0 && nowMillis - lastBusyMillis >= minutes * 60_000

private const val THERMAL_SEVERE = 3 // PowerManager.THERMAL_STATUS_SEVERE
