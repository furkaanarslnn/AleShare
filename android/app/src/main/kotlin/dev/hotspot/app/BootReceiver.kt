package dev.hotspot.app

import android.content.BroadcastReceiver
import android.content.Context
import android.content.Intent
import android.net.VpnService

/**
 * After the device starts (or the app was updated), the connection to another phone comes back if the user had
 * left it on. Sharing comes back only after an update that cut it short: the devices that use it (this computer's
 * internet, say) would otherwise be cut off until someone notices. After a restart of the phone it does not, as it
 * uses the battery and mobile data: it starts by hand then.
 */
class BootReceiver : BroadcastReceiver() {
  override fun onReceive(context: Context, intent: Intent) {
    when (intent.action) {
      Intent.ACTION_BOOT_COMPLETED, Intent.ACTION_MY_PACKAGE_REPLACED -> Unit
      else -> return
    }
    ShareTileService.ensureEnabled(context)
    // After a restart sharing is off, whatever the mark from before says: a later update must not start it.
    if (intent.action == Intent.ACTION_BOOT_COMPLETED) GatewayService.forgetSharing(context)
    if (sharingAfter(intent.action, GatewayService.wasSharing(context))) {
      // Android lets an app start its foreground service right after it was updated.
      runCatching { GatewayService.start(context, ownNetwork = ownNetworkPreference(context)) }
    }
    // The VPN consent must still be there; asking for it needs a screen, so without it nothing starts.
    if (ClientService.wanted(context) && ClientStore.load(context) != null && VpnService.prepare(context) == null) {
      ClientService.start(context)
    }
  }
}

/** Whether sharing starts again on this broadcast: after an update that ended it, never after a restart. */
internal fun sharingAfter(action: String?, wasSharing: Boolean): Boolean =
  wasSharing && action == Intent.ACTION_MY_PACKAGE_REPLACED
