package dev.hotspot.app.ui

import androidx.compose.foundation.Canvas
import androidx.compose.foundation.background
import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Box
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.Spacer
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.height
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.layout.size
import androidx.compose.foundation.layout.width
import androidx.compose.foundation.shape.CircleShape
import androidx.compose.foundation.shape.RoundedCornerShape
import androidx.compose.material3.FilledTonalButton
import androidx.compose.material3.HorizontalDivider
import androidx.compose.material3.Icon
import androidx.compose.material3.LinearProgressIndicator
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.Surface
import androidx.compose.material3.Text
import androidx.compose.material3.TextButton
import androidx.compose.runtime.Composable
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.draw.clip
import androidx.compose.ui.geometry.CornerRadius
import androidx.compose.ui.geometry.Offset
import androidx.compose.ui.geometry.Size
import androidx.compose.ui.graphics.Color
import androidx.compose.ui.graphics.PathEffect
import androidx.compose.ui.graphics.StrokeCap
import androidx.compose.ui.graphics.drawscope.Stroke
import androidx.compose.ui.graphics.vector.ImageVector
import androidx.compose.ui.text.style.TextAlign
import androidx.compose.ui.text.style.TextOverflow
import androidx.compose.ui.unit.Dp
import androidx.compose.ui.unit.dp
import dev.hotspot.app.DataUsage
import dev.hotspot.app.Plan
import dev.hotspot.app.SpeedTest
import dev.hotspot.app.TrafficStats
import dev.hotspot.app.Tr
import dev.hotspot.app.bitRate
import dev.hotspot.app.bottleneck
import dev.hotspot.app.humanBytes
import dev.hotspot.app.limitLabel
import dev.hotspot.app.ui.theme.HotspotTheme
import java.time.format.DateTimeFormatter

/** A device's share of today's data, for the breakdown: the biggest first, the rest of them as one. */
internal data class Share(val name: String, val bytes: Long)

/** The [most] biggest users of the day, then everyone else as "Diğer"; devices that used nothing are left out. */
internal fun dayShares(devices: Map<String, Long>, most: Int = 4): List<Share> {
  val used = devices.filter { it.value > 0 }.entries.sortedByDescending { it.value }.map { Share(it.key, it.value) }
  if (used.size <= most) return used
  return used.take(most) + Share("Diğer", used.drop(most).sumOf { it.bytes })
}

/** "%45" of [whole]; "<%1" for a share that is there but under one percent. */
internal fun percentLabel(part: Long, whole: Long): String {
  val p = part * 100 / whole.coerceAtLeast(1)
  return if (p == 0L && part > 0) "<%1" else "%$p"
}

/** How a measured download speed feels, in a word. */
internal fun speedWord(bps: Long): String = when {
  bps >= 50_000_000 -> "Çok hızlı"
  bps >= 20_000_000 -> "Hızlı"
  bps >= 5_000_000 -> "Orta"
  else -> "Yavaş"
}

/** "3 dk önce" for a time in Unix seconds. */
internal fun ago(unixSeconds: Long, now: Long = System.currentTimeMillis() / 1000): String {
  val minutes = (now - unixSeconds).coerceAtLeast(0) / 60
  return when {
    minutes < 1 -> "Az önce"
    minutes < 60 -> "$minutes dk önce"
    minutes < 24 * 60 -> "${minutes / 60} saat önce"
    else -> "${minutes / (24 * 60)} gün önce"
  }
}

/** The colours the devices get in the breakdown, in turn. */
@Composable
private fun shareColors(): List<Color> {
  val scheme = MaterialTheme.colorScheme
  val status = HotspotTheme.status
  return listOf(scheme.primary, status.ok, status.warn, Color(0xFFB39DFF), scheme.outline)
}

/** A ring that fills to [share] (0 to 1), with [content] in its middle. */
@Composable
private fun Ring(share: Float, color: Color, size: Dp, stroke: Dp = 8.dp, content: @Composable () -> Unit) {
  val track = MaterialTheme.colorScheme.surfaceVariant
  Box(modifier = Modifier.size(size), contentAlignment = Alignment.Center) {
    Canvas(modifier = Modifier.size(size)) {
      val w = stroke.toPx()
      val inset = w / 2
      val arc = Size(this.size.width - w, this.size.height - w)
      drawArc(track, -90f, 360f, false, Offset(inset, inset), arc, style = Stroke(w))
      if (share > 0f) drawArc(color, -90f, 360f * share.coerceIn(0f, 1f), false, Offset(inset, inset), arc, style = Stroke(w, cap = StrokeCap.Round))
    }
    content()
  }
}

/** A word or two in a pill, in [color]. */
@Composable
private fun Pill(text: String, color: Color, icon: ImageVector? = null) {
  Surface(shape = CircleShape, color = color.copy(alpha = 0.14f)) {
    Row(modifier = Modifier.padding(horizontal = 10.dp, vertical = 4.dp), verticalAlignment = Alignment.CenterVertically, horizontalArrangement = Arrangement.spacedBy(5.dp)) {
      icon?.let { Icon(it, contentDescription = null, modifier = Modifier.size(13.dp), tint = color) }
      Text(text, style = MaterialTheme.typography.labelMedium, color = color)
    }
  }
}

/**
 * Today's shared data: the total, against the daily limit as a ring when there is one, the button that sets the
 * limit, and which devices used it, as one bar in their colours with a line for each.
 */
@Composable
fun TodayPanel(usage: DataUsage.Today, limitMb: Long, onLimit: () -> Unit, locked: Boolean, overWifi: Boolean) {
  val limit = limitMb * 1024 * 1024
  val share = if (limit > 0) (usage.total.toFloat() / limit).coerceIn(0f, 1f) else 0f
  val over = limit > 0 && usage.total >= limit
  val warn = HotspotTheme.status.warn
  Panel {
    Row(verticalAlignment = Alignment.CenterVertically) {
      Column(modifier = Modifier.weight(1f)) {
        Caption("Bugün paylaşılan")
        Text(humanBytes(usage.total), style = MaterialTheme.typography.headlineMedium, color = MaterialTheme.colorScheme.onSurface)
        Spacer(Modifier.height(2.dp))
        Caption(
          when {
            over -> "Günlük sınır doldu"
            limit > 0 -> "Sınır ${limitLabel(limitMb)} · ${humanBytes(limit - usage.total)} kaldı"
            else -> "Günlük sınır yok · gece yarısı sıfırlanır"
          }
        )
      }
      if (limit > 0) {
        Spacer(Modifier.width(12.dp))
        Ring(share, if (over || share >= 0.9f) warn else MaterialTheme.colorScheme.primary, size = 72.dp) {
          Text("%${(share * 100).toInt()}", style = MaterialTheme.typography.titleMedium, color = MaterialTheme.colorScheme.onSurface)
        }
      }
    }
    val shares = dayShares(usage.devices)
    if (shares.isNotEmpty() && usage.total > 0) {
      val colors = shareColors()
      val whole = shares.sumOf { it.bytes }.coerceAtLeast(1)
      Spacer(Modifier.height(16.dp))
      Row(modifier = Modifier.fillMaxWidth().height(10.dp).clip(RoundedCornerShape(5.dp)), horizontalArrangement = Arrangement.spacedBy(2.dp)) {
        shares.forEachIndexed { i, s ->
          Box(modifier = Modifier.weight(maxOf(s.bytes.toFloat() / whole, 0.015f)).height(10.dp).background(colors[i % colors.size]))
        }
      }
      Spacer(Modifier.height(10.dp))
      shares.forEachIndexed { i, s ->
        Row(modifier = Modifier.fillMaxWidth().padding(vertical = 3.dp), verticalAlignment = Alignment.CenterVertically) {
          Box(modifier = Modifier.size(10.dp).background(colors[i % colors.size], CircleShape))
          Spacer(Modifier.width(10.dp))
          Text(s.name, style = MaterialTheme.typography.bodyMedium, color = MaterialTheme.colorScheme.onSurface, modifier = Modifier.weight(1f), maxLines = 1, overflow = TextOverflow.Ellipsis)
          Text(humanBytes(s.bytes), style = MaterialTheme.typography.labelLarge, color = MaterialTheme.colorScheme.onSurface)
          Text(
            percentLabel(s.bytes, whole),
            style = MaterialTheme.typography.bodySmall,
            color = MaterialTheme.colorScheme.onSurfaceVariant,
            textAlign = TextAlign.End,
            modifier = Modifier.width(48.dp),
          )
        }
      }
    }
    if (over) {
      Spacer(Modifier.height(10.dp))
      Banner(Tone.Warning, AppIcons.Alert, "Bugünkü sınır doldu ve paylaşım durduruldu. Yeniden başlatırsan bugün bir daha durmaz.")
    }
    if (overWifi) {
      Spacer(Modifier.height(10.dp))
      Caption("Şu an telefonun Wi-Fi'si paylaşılıyor: bu veri mobil veri değildir, burada sayılmaz ve sınırlara girmez.")
    }
    Spacer(Modifier.height(12.dp))
    FilledTonalButton(onClick = onLimit, enabled = !locked, modifier = Modifier.fillMaxWidth().height(44.dp), shape = MaterialTheme.shapes.small) {
      Text(if (limitMb > 0) "Günlük sınırı değiştir" else "Günlük sınır koy", style = MaterialTheme.typography.labelLarge)
    }
  }
}

/**
 * The monthly plan: what is used and left of it as a ring, the days to its renewal and what each day may use; then
 * the last 30 days as bars with their average.
 */
@Composable
fun PlanPanel(plan: Plan.Status?, days: List<Plan.Day>, usageAccess: Boolean, onEdit: () -> Unit, onAllow: () -> Unit, locked: Boolean) {
  Panel {
    Row(modifier = Modifier.fillMaxWidth(), verticalAlignment = Alignment.CenterVertically) {
      PanelTitle("Aylık paket", Modifier.weight(1f))
      TextButton(onClick = onEdit, enabled = !locked) { Text(if (plan == null) "Paketi gir" else "Değiştir", style = MaterialTheme.typography.labelLarge) }
    }
    if (plan == null) {
      Caption("Paketinin kaç GB olduğunu ve ayın kaçında yenilendiğini gir. Kalan veriyi ve paketin yetmesi için günde ne kadar kullanabileceğini burada görürsün.")
    } else {
      val limit = plan.limitMb * 1024 * 1024
      val share = (plan.used.toFloat() / limit).coerceIn(0f, 1f)
      val over = plan.used >= limit
      Spacer(Modifier.height(8.dp))
      Row(verticalAlignment = Alignment.CenterVertically) {
        Ring(share, if (over || share >= 0.9f) HotspotTheme.status.warn else MaterialTheme.colorScheme.primary, size = 96.dp, stroke = 10.dp) {
          Column(horizontalAlignment = Alignment.CenterHorizontally) {
            Text("%${(share * 100).toInt()}", style = MaterialTheme.typography.titleLarge, color = MaterialTheme.colorScheme.onSurface)
            Caption("kullanıldı")
          }
        }
        Spacer(Modifier.width(18.dp))
        Column(modifier = Modifier.weight(1f), verticalArrangement = Arrangement.spacedBy(8.dp)) {
          Fact("Kullanılan", "${humanBytes(plan.used)} / ${limitLabel(plan.limitMb)}")
          if (over) Fact("Paket", "Bitti") else Fact("Kalan", humanBytes(limit - plan.used))
          Fact(if (over) "Yenilenmesine" else "Günde en fazla", if (over) "${plan.daysLeft} gün" else humanBytes(plan.perDay))
        }
      }
      if (!over) {
        Spacer(Modifier.height(10.dp))
        Caption("Yenilenme: ${plan.next.format(DateTimeFormatter.ofPattern("d MMMM", Tr.locale))} · ${plan.daysLeft} gün kaldı")
      }
    }
    if (days.any { it.shared > 0 || (it.phone ?: 0) > 0 }) {
      Spacer(Modifier.height(16.dp))
      HorizontalDivider(color = MaterialTheme.colorScheme.outlineVariant)
      Spacer(Modifier.height(14.dp))
      UsageChart(days, whole = usageAccess)
    }
    if (!usageAccess) {
      Spacer(Modifier.height(10.dp))
      Row(verticalAlignment = Alignment.CenterVertically) {
        Caption("Şu an yalnızca paylaşılan veri sayılıyor. Telefonun kendi kullanımı da sayılsın istersen kullanım erişimi izni ver.", Modifier.weight(1f))
        TextButton(onClick = onAllow, enabled = !locked) { Text("İzin ver", style = MaterialTheme.typography.labelLarge) }
      }
    }
  }
}

/** A label and its value, one under the other. */
@Composable
private fun Fact(label: String, value: String) {
  Column {
    Caption(label)
    Text(value, style = MaterialTheme.typography.titleSmall, color = MaterialTheme.colorScheme.onSurface)
  }
}

/**
 * The last days as bars, today last and marked: the whole phone faint (when it can be read, [whole]), sharing's
 * part strong, and the days' average as a dashed line.
 */
@Composable
private fun UsageChart(days: List<Plan.Day>, whole: Boolean) {
  val strong = MaterialTheme.colorScheme.primary
  val faint = MaterialTheme.colorScheme.primary.copy(alpha = 0.28f)
  val track = MaterialTheme.colorScheme.outlineVariant
  val label = MaterialTheme.colorScheme.onSurfaceVariant
  val totals = days.map { maxOf(it.shared, it.phone ?: 0) }
  val top = totals.max().coerceAtLeast(1)
  val average = totals.sum() / days.size.coerceAtLeast(1)
  Column {
    Row(verticalAlignment = Alignment.CenterVertically) {
      Text("Son ${days.size} gün", style = MaterialTheme.typography.titleSmall, color = MaterialTheme.colorScheme.onSurface, modifier = Modifier.weight(1f))
      Caption("Günde ortalama ${humanBytes(average)}")
    }
    Spacer(Modifier.height(10.dp))
    Canvas(modifier = Modifier.fillMaxWidth().height(96.dp)) {
      val slot = size.width / days.size
      val bar = slot * 0.6f
      val r = CornerRadius(bar / 2.5f)
      drawLine(track, Offset(0f, size.height), Offset(size.width, size.height), strokeWidth = 1.dp.toPx())
      days.forEachIndexed { i, d ->
        val x = i * slot + (slot - bar) / 2
        val today = i == days.lastIndex
        val all = totals[i]
        if (all > 0) {
          val h = (all.toFloat() / top * size.height).coerceAtLeast(2.dp.toPx())
          drawRoundRect(if (today) strong.copy(alpha = 0.45f) else faint, Offset(x, size.height - h), Size(bar, h), r)
        }
        if (d.shared > 0) {
          val h = (d.shared.toFloat() / top * size.height).coerceAtLeast(2.dp.toPx())
          drawRoundRect(strong, Offset(x, size.height - h), Size(bar, h), r)
        }
      }
      if (average > 0) {
        val y = size.height - average.toFloat() / top * size.height
        drawLine(label.copy(alpha = 0.7f), Offset(0f, y), Offset(size.width, y), strokeWidth = 1.dp.toPx(), pathEffect = PathEffect.dashPathEffect(floatArrayOf(6.dp.toPx(), 4.dp.toPx())))
      }
    }
    Spacer(Modifier.height(6.dp))
    Row(modifier = Modifier.fillMaxWidth()) {
      Caption("${days.first().date.dayOfMonth}.${days.first().date.monthValue}", Modifier.weight(1f))
      Caption("En yüksek ${humanBytes(top)}", Modifier.weight(1f).padding(start = 8.dp))
      Caption("Bugün")
    }
    Spacer(Modifier.height(10.dp))
    Row(horizontalArrangement = Arrangement.spacedBy(16.dp), verticalAlignment = Alignment.CenterVertically) {
      Legend(strong, "Paylaşılan")
      if (whole) Legend(faint, "Telefonun kendi kullanımı")
    }
  }
}

@Composable
private fun Legend(color: Color, text: String) {
  Row(verticalAlignment = Alignment.CenterVertically, horizontalArrangement = Arrangement.spacedBy(6.dp)) {
    Box(modifier = Modifier.size(10.dp).background(color, RoundedCornerShape(3.dp)))
    Caption(text)
  }
}

/** The speed test of the phone's internet: the last result with its download speed first, and the button. */
@Composable
fun SpeedPanel(speed: SpeedTest.State, onStart: () -> Unit) {
  Panel {
    Row(modifier = Modifier.fillMaxWidth(), verticalAlignment = Alignment.CenterVertically) {
      PanelTitle("Hız testi", Modifier.weight(1f))
      FilledTonalButton(onClick = onStart, enabled = !speed.running, modifier = Modifier.height(36.dp), shape = MaterialTheme.shapes.small) {
        Text(if (speed.running) "Ölçülüyor…" else if (speed.last == null) "Ölç" else "Yeniden ölç", style = MaterialTheme.typography.labelLarge)
      }
    }
    if (speed.running) {
      Spacer(Modifier.height(12.dp))
      LinearProgressIndicator(modifier = Modifier.fillMaxWidth(), trackColor = MaterialTheme.colorScheme.surfaceVariant)
      speed.stage?.let {
        Spacer(Modifier.height(6.dp))
        Caption(it)
      }
    }
    speed.last?.let { r ->
      Spacer(Modifier.height(12.dp))
      Row(verticalAlignment = Alignment.Bottom) {
        Column(modifier = Modifier.weight(1f)) {
          Row(verticalAlignment = Alignment.CenterVertically, horizontalArrangement = Arrangement.spacedBy(6.dp)) {
            Icon(AppIcons.ArrowDown, contentDescription = null, modifier = Modifier.size(14.dp), tint = MaterialTheme.colorScheme.onSurfaceVariant)
            Caption("İndirme")
          }
          Text(bitRate(r.downBps), style = MaterialTheme.typography.headlineMedium, color = MaterialTheme.colorScheme.onSurface)
        }
        Pill(speedWord(r.downBps), if (r.downBps >= 5_000_000) HotspotTheme.status.ok else HotspotTheme.status.warn)
      }
      Spacer(Modifier.height(12.dp))
      Surface(shape = MaterialTheme.shapes.medium, color = MaterialTheme.colorScheme.surfaceVariant, modifier = Modifier.fillMaxWidth()) {
        Row(modifier = Modifier.padding(horizontal = 14.dp, vertical = 10.dp)) {
          Metric(AppIcons.ArrowUp, "Yükleme", bitRate(r.upBps), Modifier.weight(1f))
          Metric(AppIcons.Link, "Ping", "${r.pingMs} ms", Modifier.weight(1f))
          if (r.wifiDownBps > 0) Metric(AppIcons.Wifi, "Aradaki Wi-Fi", bitRate(r.wifiDownBps), Modifier.weight(1.2f))
        }
      }
      if (r.wifiDownBps > 0) {
        bottleneck(r)?.let {
          Spacer(Modifier.height(8.dp))
          Caption(it)
        }
      }
      Spacer(Modifier.height(8.dp))
      Caption("${ago(r.at)} ölçüldü · ${humanBytes(r.bytes)} veri harcandı")
    }
    speed.error?.let {
      Spacer(Modifier.height(8.dp))
      Banner(Tone.Warning, AppIcons.Alert, it)
    }
    if (speed.last == null && speed.error == null && !speed.running) {
      Spacer(Modifier.height(6.dp))
      Caption("Telefonun paylaştığı internetin (mobil veri ya da Wi-Fi) hızını ölçer; en fazla yaklaşık 55 MB veri harcar.")
    }
  }
}

/** This sharing so far: what went each way, the connections open now, and how the names were looked up. */
@Composable
fun SessionPanel(t: TrafficStats) {
  val status = HotspotTheme.status
  Panel {
    Row(verticalAlignment = Alignment.CenterVertically) {
      PanelTitle("Bu oturum", Modifier.weight(1f))
      when {
        t.dnsPlain > 0 -> Pill("DNS şifresiz", status.warn, AppIcons.Alert)
        t.dnsEncrypted > 0 -> Pill("DNS şifreli", status.ok, AppIcons.Shield)
      }
    }
    Spacer(Modifier.height(12.dp))
    Row(modifier = Modifier.fillMaxWidth()) {
      Metric(AppIcons.ArrowDown, "İndirme", humanBytes(t.bytesDown), Modifier.weight(1f))
      Metric(AppIcons.ArrowUp, "Yükleme", humanBytes(t.bytesUp), Modifier.weight(1f))
      Metric(AppIcons.Link, "Bağlantı", (t.tcpActive + t.udpActive).toString(), Modifier.weight(0.8f))
    }
    val notes = listOfNotNull(
      t.dnsBlocked.takeIf { it > 0 }?.let { "$it reklam ve izleyici isteği engellendi" },
      t.dnsPlain.takeIf { it > 0 }?.let { "$it DNS sorgusu şifresiz gitti (şifreli sunuculara ulaşılamadı)" },
      t.dialErrors.takeIf { it > 0 }?.let { "Ulaşılamayan adres: $it" },
    )
    if (notes.isNotEmpty()) {
      Spacer(Modifier.height(12.dp))
      notes.forEach { Caption(it) }
    }
  }
}
