package dev.hotspot.app

import android.content.Context
import dev.hotspot.wgcore.Join
import dev.hotspot.wgcore.Wgcore
import kotlinx.coroutines.flow.MutableStateFlow
import kotlinx.coroutines.flow.StateFlow
import kotlinx.coroutines.flow.asStateFlow
import kotlinx.coroutines.flow.update

/** Where the connection to a phone that shares stands. */
enum class ClientPhase {
  /** Not running. */
  Idle,

  /** Waiting for the phone's Wi-Fi network to come into range, or for the user to allow joining it. */
  Searching,

  /** On the network, the tunnel is opening. */
  Connecting,

  /** The phone has answered: the internet comes through it. */
  Connected,

  /** The network is there but the phone's app does not answer; the tunnel is down so the normal internet works. */
  Unreachable,
}

data class ClientUiState(
  val phase: ClientPhase = ClientPhase.Idle,
  val ssid: String? = null,
  val rx: Long = 0,
  val tx: Long = 0,
  val error: String? = null,
  /** A newer app that the phone offers, and how far taking it has come; null when there is none. */
  val update: AppUpdate? = null,
)

/**
 * The phone runs a newer release ([name], [code]) than this device and hands its APK out at [url]. [progress] is
 * the share downloaded (0..100) while downloading, [file] the APK once it is here, [error] why it is not.
 */
data class AppUpdate(
  val name: String,
  val code: Long,
  val url: String,
  val progress: Int? = null,
  val file: String? = null,
  val error: String? = null,
)

object ClientState {
  private val _state = MutableStateFlow(ClientUiState())
  val state: StateFlow<ClientUiState> = _state.asStateFlow()

  fun update(block: (ClientUiState) -> ClientUiState) = _state.update(block)
}

/**
 * The phone this device connects to, as its join link. The link holds this device's private key, so it is
 * kept encrypted like the core's state, and never leaves the app.
 */
object ClientStore {
  private const val ENTRY = "client_join"
  private const val NEXT_ENTRY = "client_next_wifi"

  fun load(context: Context): Join? =
    SecureStore.get(context, ENTRY)?.let { link -> runCatching { Wgcore.parseJoin(link) }.getOrNull() }

  fun save(context: Context, link: String) {
    SecureStore.put(context, ENTRY, link)
    setNext(context, null) // a new phone, or the same one anew: nothing is changing
  }

  fun clear(context: Context) {
    SecureStore.remove(context, ENTRY)
    setNext(context, null)
  }

  /**
   * Gives the saved connection another Wi-Fi network: the phone's network got a new name or password. The tunnel
   * (and this device's key in it) stays the same. Returns the connection as it is now, or null if none is saved.
   */
  fun updateWifi(context: Context, wifi: WifiCredentials): Join? {
    val j = load(context) ?: return null
    if (j.ssid == wifi.ssid && j.password == wifi.passphrase) return j
    val link = runCatching { Wgcore.encodeJoin(wifi.ssid, wifi.passphrase, j.config) }.getOrNull() ?: return null
    SecureStore.put(context, ENTRY, link)
    return runCatching { Wgcore.parseJoin(link) }.getOrNull()
  }

  /** The network the phone said it changes to; this device looks for both until it knows which one is there. */
  fun next(context: Context): WifiCredentials? =
    SecureStore.get(context, NEXT_ENTRY)?.lines()?.takeIf { it.size >= 2 }?.let { WifiCredentials(it[0], it[1]) }

  fun setNext(context: Context, next: WifiCredentials?) {
    if (next == null) SecureStore.remove(context, NEXT_ENTRY) else SecureStore.put(context, NEXT_ENTRY, next.ssid + "\n" + next.passphrase)
  }
}

/** A join link that arrived from outside (a tapped link), waiting for the screen to ask the user about it. */
object JoinInbox {
  private val _link = MutableStateFlow<String?>(null)
  val link: StateFlow<String?> = _link.asStateFlow()

  fun offer(link: String?) {
    if (link != null) _link.value = link
  }

  fun consume() {
    _link.value = null
  }
}
