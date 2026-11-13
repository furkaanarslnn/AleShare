package dev.hotspot.app

import android.app.Notification
import android.app.PendingIntent
import android.content.Context
import androidx.core.app.NotificationCompat
import androidx.core.content.ContextCompat

/**
 * What a running connection's notification says: [title], the line under it ([text]), and the lines of its
 * expanded form ([big], none for a short one).
 */
data class LiveNote(val title: String, val text: String, val big: String? = null)

/** The sharing phone's notification: how many devices use it, how fast it goes now, and what it shared. */
internal fun shareNote(devices: Int, downBps: Long, upBps: Long, today: Long, session: Long): LiveNote =
  LiveNote(
    title = if (devices > 0) "$devices cihaz bağlı" else "Paylaşım açık",
    text = "${speedLine(downBps, upBps)}  ·  Bugün ${humanBytes(today)}",
    big = "İndirme ${bitRate(downBps * 8)}  ·  Yükleme ${bitRate(upBps * 8)}\n" +
      "Bugün paylaşılan ${humanBytes(today)}  ·  bu oturumda ${humanBytes(session)}" +
      if (devices == 0) "\nCihazlar bağlanınca burada görünür." else "",
  )

/** A connected device's notification: connected, how fast now, and what came and went through the tunnel. */
internal fun clientNote(downBps: Long, upBps: Long, received: Long, sent: Long): LiveNote =
  LiveNote(
    title = "Bağlanıldı",
    text = speedLine(downBps, upBps),
    big = "İndirme ${bitRate(downBps * 8)}  ·  Yükleme ${bitRate(upBps * 8)}\n" +
      "İndirilen ${humanBytes(received)}  ·  Yüklenen ${humanBytes(sent)}",
  )

/** "↓ 4,2 Mbit/s   ↑ 310 kbit/s" for bytes per second. */
internal fun speedLine(downBps: Long, upBps: Long) = "↓ ${bitRate(downBps * 8)}   ↑ ${bitRate(upBps * 8)}"

/** The bytes per second between two readings of a counter [millis] apart; 0 before there are two. */
internal fun perSecond(now: Long, before: Long?, millis: Long): Long =
  if (before == null || millis <= 0) 0 else (now - before).coerceAtLeast(0) * 1000 / millis

/**
 * A running connection's notification, the same for sharing and for a connected device: in the app's colour (the
 * system colours it for a foreground service), counting the time since [since] (epoch millis, 0 for none), silent,
 * with one button to stop.
 */
internal fun liveNotification(
  context: Context,
  channel: String,
  note: LiveNote,
  since: Long,
  open: PendingIntent,
  stop: PendingIntent,
): Notification =
  NotificationCompat.Builder(context, channel)
    .setSmallIcon(R.drawable.ic_stat_hotspot)
    .setColor(ContextCompat.getColor(context, R.color.accent))
    .setColorized(true)
    .setContentTitle(note.title)
    .setContentText(note.text)
    .apply { note.big?.let { setStyle(NotificationCompat.BigTextStyle().bigText(it)) } }
    .apply {
      if (since > 0) {
        setWhen(since)
        setUsesChronometer(true)
        setShowWhen(true)
      } else {
        setShowWhen(false)
      }
    }
    .setContentIntent(open)
    .addAction(0, "Durdur", stop)
    .setOngoing(true)
    .setOnlyAlertOnce(true)
    .setSilent(true)
    .setCategory(NotificationCompat.CATEGORY_SERVICE)
    .setVisibility(NotificationCompat.VISIBILITY_PUBLIC)
    .setForegroundServiceBehavior(NotificationCompat.FOREGROUND_SERVICE_IMMEDIATE)
    .build()
