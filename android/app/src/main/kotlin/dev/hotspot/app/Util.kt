package dev.hotspot.app

import android.graphics.Bitmap
import android.graphics.Color
import com.google.zxing.BarcodeFormat
import com.google.zxing.EncodeHintType
import com.google.zxing.qrcode.QRCodeWriter
import com.google.zxing.qrcode.decoder.ErrorCorrectionLevel
import dev.hotspot.wgcore.Stats
import java.net.Inet4Address
import java.net.NetworkInterface

/** A private IPv4 address of this phone that a paired device could connect to. */
data class LocalAddress(val ifName: String, val ip: String) {
  /** Interfaces that carry a hotspot, Wi-Fi Direct group or USB tethering. */
  val isShareLink: Boolean
    get() = ifName.startsWith("ap") || ifName.startsWith("swlan") || ifName.startsWith("p2p") ||
      ifName.startsWith("rndis") || ifName.startsWith("wlan1") || ifName.startsWith("softap")
}

private val IGNORED_INTERFACES = listOf("rmnet", "ccmni", "tun", "v4-", "clat", "dummy", "lo")

fun localAddresses(): List<LocalAddress> {
  val interfaces = try {
    NetworkInterface.getNetworkInterfaces()?.toList().orEmpty()
  } catch (_: Exception) {
    emptyList()
  }
  return interfaces
    .filter { itf -> runCatching { itf.isUp && !itf.isLoopback }.getOrDefault(false) }
    .filterNot { itf -> IGNORED_INTERFACES.any { itf.name.startsWith(it) } }
    .flatMap { itf ->
      itf.inetAddresses.toList()
        .filterIsInstance<Inet4Address>()
        .filter { it.isSiteLocalAddress }
        .mapNotNull { addr -> addr.hostAddress?.let { LocalAddress(itf.name, it) } }
    }
    .sortedByDescending { it.isShareLink }
}

fun qrBitmap(text: String, size: Int = 720): Bitmap {
  val hints = mapOf(EncodeHintType.MARGIN to 2, EncodeHintType.ERROR_CORRECTION to ErrorCorrectionLevel.L)
  val matrix = QRCodeWriter().encode(text, BarcodeFormat.QR_CODE, size, size, hints)
  val pixels = IntArray(size * size)
  for (y in 0 until size) {
    for (x in 0 until size) pixels[y * size + x] = if (matrix[x, y]) Color.BLACK else Color.WHITE
  }
  return Bitmap.createBitmap(pixels, size, size, Bitmap.Config.ARGB_8888)
}

/**
 * What the release with the number [code] is called: "1.0.N" up to 38, "1.1" for 39 (the first one published),
 * "1.2" for 40, then "1.2.1", "1.2.2" and so on. The same rule as release.Name in the Go core.
 */
fun releaseName(code: Long): String = when {
  code < 39 -> "1.0.$code"
  code == 39L -> "1.1"
  code == 40L -> "1.2"
  else -> "1.2.${code - 40}"
}

/** This app's release number (versionCode), to compare with the one a phone offers. */
fun ownVersionCode(context: android.content.Context): Long =
  androidx.core.content.pm.PackageInfoCompat.getLongVersionCode(context.packageManager.getPackageInfo(context.packageName, 0))

data class TrafficStats(
  val tcpActive: Long,
  val tcpTotal: Long,
  val udpActive: Long,
  val udpTotal: Long,
  val dialErrors: Long,
  val bytesUp: Long,
  val bytesDown: Long,
  /** DNS queries answered over HTTPS, and in plain (when no encrypted server could be reached). */
  val dnsEncrypted: Long,
  val dnsPlain: Long,
  /** Connections whose TLS handshake went out in pieces ("AleDPI modu"). */
  val tcpSplit: Long,
  /** Queries for ad, tracker and update names answered with nothing. */
  val dnsBlocked: Long,
)

fun Stats.toTraffic() = TrafficStats(
  getTCPActive(), getTCPTotal(), getUDPActive(), getUDPTotal(), getDialErrors(), getBytesUp(), getBytesDown(),
  getDNSEncrypted(), getDNSPlain(), getTCPSplit(), getDNSBlocked(),
)

/** An amount of data the way the Windows program writes it too: "850 KB", "120 MB", "1,5 GB" (decimal comma). */
fun humanBytes(n: Long): String {
  val mb = 1024.0 * 1024
  return when {
    n < 1024 -> "$n B"
    n < mb -> "${Math.round(n / 1024.0)} KB"
    n < 1024 * mb -> "${Math.round(n / mb)} MB"
    n < 100 * 1024 * mb -> String.format(Tr.locale, "%.1f GB", n / (1024 * mb))
    else -> "${Math.round(n / (1024 * mb))} GB"
  }
}
