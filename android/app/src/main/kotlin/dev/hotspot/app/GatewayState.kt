package dev.hotspot.app

import kotlinx.coroutines.flow.MutableStateFlow
import kotlinx.coroutines.flow.StateFlow
import kotlinx.coroutines.flow.asStateFlow
import kotlinx.coroutines.flow.update

/** What the UI needs to know about the service. Traffic counters are read from the core on demand. */
data class GatewayUiState(
  val running: Boolean = false,
  val starting: Boolean = false,
  val cellular: Boolean = false,
  val port: Int = GatewayService.DEFAULT_PORT,
  /** Name of the Wi-Fi network the app created, if it did. */
  val network: String? = null,
  /** A change of the network's name or password that waits for the connected devices to learn it. */
  val wifiChange: WifiChange? = null,
  val error: String? = null,
  /** A newer release of this app that a paired device brought, waiting to be installed here too. */
  val pendingApp: Updates.Apk? = null,
  /** The band and channel the app's own network is on ("5 GHz · kanal 36"), when known. */
  val band: String? = null,
)

/** A Wi-Fi frequency in MHz as the user reads it: band and channel; null when it is not a Wi-Fi channel. */
fun bandLabel(mhz: Int): String? = when (mhz) {
  in 2412..2472 -> "2,4 GHz · kanal ${(mhz - 2407) / 5}"
  2484 -> "2,4 GHz · kanal 14"
  in 5160..5885 -> "5 GHz · kanal ${(mhz - 5000) / 5}"
  else -> null
}

/** [waitingFor]: the devices connected when the change began; [learned]: those of them that know the new network. */
data class WifiChange(val ssid: String, val waitingFor: List<String>, val learned: List<String>)

object GatewayState {
  private val _state = MutableStateFlow(GatewayUiState())
  val state: StateFlow<GatewayUiState> = _state.asStateFlow()

  fun update(block: (GatewayUiState) -> GatewayUiState) = _state.update(block)
}
