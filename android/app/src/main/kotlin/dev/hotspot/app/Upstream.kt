package dev.hotspot.app

import android.content.Context
import android.net.ConnectivityManager
import android.net.LinkProperties
import android.net.Network
import android.net.NetworkCapabilities
import android.net.NetworkRequest
import android.os.ParcelFileDescriptor
import dev.hotspot.wgcore.Protector
import java.net.Inet4Address

/**
 * Where the shared internet comes from: the phone's mobile data, or the Wi-Fi network the phone is connected to
 * (a hotel's, a café's, home). Either way the devices get the app's own network, its encrypted DNS and its ad
 * blocking; on Wi-Fi no mobile data is used.
 */
enum class Source(val key: String) {
  Mobile("mobile"),
  Wifi("wifi");

  /** What the screens call it. */
  val label: String get() = if (this == Mobile) "Mobil veri" else "Wi-Fi"

  companion object {
    fun of(key: String?): Source? = entries.firstOrNull { it.key == key }
  }
}

/** The source the user picked; without a pick, mobile data where there is a SIM card and Wi-Fi where there is none. */
fun sourcePreference(context: Context): Source =
  Source.of(context.getSharedPreferences("settings", Context.MODE_PRIVATE).getString("source", null))
    ?: defaultSource(SimCard.present(context))

fun setSourcePreference(context: Context, source: Source) =
  context.getSharedPreferences("settings", Context.MODE_PRIVATE).edit().putString("source", source.key).apply()

internal fun defaultSource(hasSim: Boolean) = if (hasSim) Source.Mobile else Source.Wifi

/** Why sharing from [source] cannot start, or null when it can: mobile data needs a SIM card. */
internal fun sourceProblem(source: Source, hasSim: Boolean): String? =
  if (source == Source.Mobile && !hasSim) SimCard.MISSING else null

/** Whether sharing from the chosen source may start now (see [sourceProblem]). */
fun sourceUsable(context: Context): Boolean = sourceProblem(sourcePreference(context), SimCard.present(context)) == null

/**
 * Keeps the network of [source] requested while the gateway runs, and tells the screen whether it is there. A
 * Wi-Fi network that still waits for its sign-in page (hotels, cafés) is reported as such: the devices get no
 * internet before the page is done on the phone.
 */
class UpstreamNetwork(context: Context, val source: Source) {
  private val connectivity = context.getSystemService(ConnectivityManager::class.java)

  @Volatile
  var network: Network? = null
    private set

  private val callback = object : ConnectivityManager.NetworkCallback() {
    override fun onAvailable(network: Network) {
      this@UpstreamNetwork.network = network
      GatewayState.update { it.copy(online = true) }
    }

    override fun onCapabilitiesChanged(network: Network, caps: NetworkCapabilities) {
      if (this@UpstreamNetwork.network != network) return
      val captive = caps.hasCapability(NetworkCapabilities.NET_CAPABILITY_CAPTIVE_PORTAL)
      GatewayState.update { if (it.captive == captive) it else it.copy(captive = captive) }
    }

    override fun onLinkPropertiesChanged(network: Network, link: LinkProperties) {
      if (this@UpstreamNetwork.network != network) return
      val clash = link.linkAddresses.any { a -> (a.address as? Inet4Address)?.let { clashesWithOwnNetwork(it.address) } == true }
      GatewayState.update { if (it.clash == clash) it else it.copy(clash = clash) }
    }

    override fun onLost(network: Network) {
      if (this@UpstreamNetwork.network == network) {
        this@UpstreamNetwork.network = null
        GatewayState.update { it.copy(online = false, captive = false, clash = false) }
      }
    }
  }

  fun start() {
    val request = NetworkRequest.Builder()
      .addTransportType(if (source == Source.Mobile) NetworkCapabilities.TRANSPORT_CELLULAR else NetworkCapabilities.TRANSPORT_WIFI)
      .addCapability(NetworkCapabilities.NET_CAPABILITY_INTERNET)
      .build()
    if (source == Source.Mobile) {
      // Asked for: Android brings mobile data up for the app even while the phone uses Wi-Fi.
      connectivity.requestNetwork(request, callback)
    } else {
      // Only listened to: asking for Wi-Fi would have Android look for a network to join, which is the user's choice.
      connectivity.registerNetworkCallback(request, callback)
    }
  }

  fun stop() {
    try {
      connectivity.unregisterNetworkCallback(callback)
    } catch (_: IllegalArgumentException) {
      // never registered
    }
    network = null
  }
}

/**
 * Whether an IPv4 address of the phone's Wi-Fi lies in 192.168.49.0/24, the app's own network: the phone is joined
 * to another phone's Wi-Fi Direct network, whose addresses are the same as its own, and cannot share it.
 */
internal fun clashesWithOwnNetwork(a: ByteArray): Boolean =
  a.size == 4 && a[0] == 192.toByte() && a[1] == 168.toByte() && a[2] == 49.toByte()

/**
 * Binds every socket the Go core opens to the shared network, so tunnelled traffic leaves over mobile data or the
 * phone's Wi-Fi and never loops back into the app's own network.
 */
class UpstreamProtector(private val upstream: UpstreamNetwork) : Protector {
  override fun protect(fd: Long): Boolean {
    val network = upstream.network ?: return false
    if (GatewayState.state.value.clash) return false
    return try {
      // fromFd duplicates the descriptor; the socket option set on it applies to the original socket.
      ParcelFileDescriptor.fromFd(fd.toInt()).use { network.bindSocket(it.fileDescriptor) }
      true
    } catch (_: Exception) {
      false
    }
  }
}
