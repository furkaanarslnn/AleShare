package dev.hotspot.app

import java.util.Locale

/** Who made the app; shown under the version. */
const val MAKER = "furkanyela"

/**
 * Turkish endings after a number follow the way the number is read aloud: "%45'i" (kırk beşi) but "%6'sı" (altısı)
 * and "%10'u" (onu); "07:00'de" (yedide) but "07:30'da" (otuzda) and "03:00'te" (üçte). A fixed ending is wrong
 * for most numbers.
 */
object Tr {
  val locale: Locale = Locale.forLanguageTag("tr")

  private val onesIts = arrayOf("", "i", "si", "ü", "ü", "i", "sı", "si", "i", "u")
  private val tensIts = arrayOf("", "u", "si", "u", "ı", "si", "ı", "i", "i", "ı")
  private val onesAt = arrayOf("", "de", "de", "te", "te", "te", "da", "de", "de", "da")
  private val tensAt = arrayOf("", "da", "de", "da", "ta", "de", "ta", "te", "de", "da")

  /** The ending of "its N" ("%N'i kullanıldı"): i, ı, u, ü, si, sı. */
  fun its(n: Int): String = pick(n, onesIts, tensIts, hundred = "ü", zero = "ı")

  /** The ending of "at N" ("N'de"): de, da, te, ta. */
  fun at(n: Int): String = pick(n, onesAt, tensAt, hundred = "de", zero = "da")

  /** "%45'i": that share of something. */
  fun share(percent: Int): String = "%$percent'${its(percent)}"

  /** "07:30'da": at a time of the day, given in minutes; a full hour is read as the hour alone. */
  fun atClock(minutes: Int): String {
    val (h, m) = minutes / 60 to minutes % 60
    return "%02d:%02d'%s".format(h, m, at(if (m != 0) m else h))
  }

  // The last word of the number decides: its ones, else its tens, else "yüz" (and "sıfır" for nothing at all).
  private fun pick(n: Int, ones: Array<String>, tens: Array<String>, hundred: String, zero: String): String {
    val v = if (n < 0) -n else n
    return when {
      v % 10 != 0 -> ones[v % 10]
      v % 100 != 0 -> tens[v / 10 % 10]
      v != 0 -> hundred
      else -> zero
    }
  }
}
