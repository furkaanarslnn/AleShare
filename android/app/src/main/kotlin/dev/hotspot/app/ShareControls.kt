package dev.hotspot.app

import android.Manifest
import android.content.Context
import android.content.pm.PackageManager
import android.os.Build
import androidx.core.content.ContextCompat

/** What a tap on a control outside the app does. */
enum class ShareAction {
  /** Starts sharing, or stops it when it runs. */
  Toggle,

  /** Opens the app, which says what is missing (a permission, or this phone uses another phone's internet). */
  OpenApp,
}

/**
 * What the controls outside the app show: the widget [title] and [detail], the Quick Settings tile its fixed name
 * and [brief] under it.
 */
data class ShareStatus(val on: Boolean, val title: String, val detail: String, val brief: String, val action: ShareAction)

/**
 * The controls outside the app. The app tells them every change ([refresh]); they never wake the phone on their
 * own.
 */
object ShareControls {
  /** The newest count of connected devices, as the sharing service last saw it. */
  @Volatile private var connected = 0

  /** Redraws the widget and the tile; [devices] is the newest count of connected devices, when it is known. */
  fun refresh(context: Context, devices: Int? = null) {
    devices?.let { connected = it }
    ShareWidget.update(context)
    ShareTileService.update(context)
  }

  fun status(context: Context): ShareStatus {
    val s = GatewayState.state.value
    if (!s.running) connected = 0
    val devices = if (connected == 0) "Bağlı cihaz yok" else "$connected cihaz bağlı"
    return when {
      s.running -> ShareStatus(true, "Paylaşılıyor", devices, devices, ShareAction.Toggle)
      s.starting -> ShareStatus(false, "Başlatılıyor", "Wi-Fi hazırlanıyor", "Başlatılıyor", ShareAction.Toggle)
      ClientService.wanted(context) -> ShareStatus(false, "Paylaşım kapalı", "Başka bir telefona bağlı", "Başka telefona bağlı", ShareAction.OpenApp)
      !mayShare(context) -> ShareStatus(false, "Paylaşım kapalı", "İzin vermek için dokun", "İzin gerekli", ShareAction.OpenApp)
      s.error != null -> ShareStatus(false, "Paylaşım kapalı", "Başlatılamadı, yeniden dokun", "Başlatılamadı", ShareAction.Toggle)
      else -> ShareStatus(false, "Paylaşım kapalı", "Başlatmak için dokun", "Kapalı", ShareAction.Toggle)
    }
  }

  /** Whether sharing can start without asking for anything: opening the Wi-Fi network needs a permission. */
  private fun mayShare(context: Context): Boolean {
    if (!ownNetworkPreference(context)) return true
    val permission = if (Build.VERSION.SDK_INT >= 33) Manifest.permission.NEARBY_WIFI_DEVICES else Manifest.permission.ACCESS_FINE_LOCATION
    return ContextCompat.checkSelfPermission(context, permission) == PackageManager.PERMISSION_GRANTED
  }
}
