package dev.hotspot.app

import android.content.Context
import android.net.ConnectivityManager
import android.net.Network
import android.net.NetworkCapabilities
import android.net.NetworkRequest
import android.os.ParcelFileDescriptor
import dev.hotspot.wgcore.Protector

/** Keeps the cellular network requested while the gateway runs. */
class CellularNetwork(context: Context) {
  private val connectivity = context.getSystemService(ConnectivityManager::class.java)

  @Volatile
  var network: Network? = null
    private set

  private val callback = object : ConnectivityManager.NetworkCallback() {
    override fun onAvailable(network: Network) {
      this@CellularNetwork.network = network
      GatewayState.update { it.copy(cellular = true) }
    }

    override fun onLost(network: Network) {
      if (this@CellularNetwork.network == network) {
        this@CellularNetwork.network = null
        GatewayState.update { it.copy(cellular = false) }
      }
    }
  }

  fun start() {
    val request = NetworkRequest.Builder()
      .addTransportType(NetworkCapabilities.TRANSPORT_CELLULAR)
      .addCapability(NetworkCapabilities.NET_CAPABILITY_INTERNET)
      .build()
    connectivity.requestNetwork(request, callback)
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
 * Binds every socket the Go core opens to the cellular network, so tunnelled
 * traffic leaves over mobile data and never loops back into the hotspot.
 */
class CellularProtector(private val cellular: CellularNetwork) : Protector {
  override fun protect(fd: Long): Boolean {
    val network = cellular.network ?: return false
    return try {
      // fromFd duplicates the descriptor; the socket option set on it applies to the original socket.
      ParcelFileDescriptor.fromFd(fd.toInt()).use { network.bindSocket(it.fileDescriptor) }
      true
    } catch (_: Exception) {
      false
    }
  }
}
