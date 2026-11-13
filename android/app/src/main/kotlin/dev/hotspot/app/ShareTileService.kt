package dev.hotspot.app

import android.annotation.SuppressLint
import android.app.PendingIntent
import android.content.ComponentName
import android.content.Context
import android.content.Intent
import android.content.pm.PackageManager
import android.graphics.drawable.Icon
import android.os.Build
import android.os.Handler
import android.os.Looper
import android.service.quicksettings.Tile
import android.service.quicksettings.TileService
import android.util.Log

/**
 * The Quick Settings tile: a tap starts sharing, another stops it. On while sharing, with the number of connected
 * devices under its name (see [ShareControls]).
 *
 * It is an "active" tile: the app asks the system to redraw it whenever sharing changes ([update]), instead of the
 * system binding it every time the panel opens. When a tap could not start sharing (a permission is missing, this
 * phone uses another phone's internet, or Android does not let the service start now) it opens the app.
 */
class ShareTileService : TileService() {
  override fun onTileAdded() = setAdded(this, true)

  override fun onTileRemoved() = setAdded(this, false)

  override fun onStartListening() {
    listening = this
    setAdded(this, true) // only a tile in the panel is listened to
    render()
  }

  override fun onStopListening() {
    if (listening === this) listening = null
  }

  override fun onDestroy() {
    if (listening === this) listening = null
    super.onDestroy()
  }

  override fun onClick() {
    val state = GatewayState.state.value
    when {
      state.running || state.starting -> {
        GatewayService.stop(this)
        show(active = false, brief = "Kapalı")
      }
      ShareControls.status(this).action == ShareAction.OpenApp -> openApp()
      else -> try {
        GatewayService.start(this, ownNetwork = ownNetworkPreference(this))
        // The service starts a moment later: show the answer to the tap at once, the service corrects it.
        show(active = true, brief = "Başlatılıyor")
      } catch (e: Exception) {
        // Android 12+ refuses a foreground service it does not see the user asking for: the app starts it then.
        Log.w(TAG, "could not start sharing from the tile", e)
        openApp()
      }
    }
  }

  private fun render() {
    val s = ShareControls.status(this)
    show(active = s.on || GatewayState.state.value.starting, brief = s.brief)
  }

  private fun show(active: Boolean, brief: String) {
    val tile = qsTile ?: return
    tile.state = if (active) Tile.STATE_ACTIVE else Tile.STATE_INACTIVE
    tile.label = "Paylaşım"
    tile.icon = Icon.createWithResource(this, R.drawable.ic_hotspot)
    if (Build.VERSION.SDK_INT >= 29) tile.subtitle = brief
    tile.contentDescription = "Paylaşım. $brief"
    tile.updateTile()
  }

  /** Opens the app over the lock screen's unlock, and folds the panel away. */
  @SuppressLint("StartActivityAndCollapseDeprecated")
  private fun openApp() {
    val intent = Intent(this, MainActivity::class.java).addFlags(Intent.FLAG_ACTIVITY_NEW_TASK)
    val open = {
      if (Build.VERSION.SDK_INT >= 34) {
        startActivityAndCollapse(PendingIntent.getActivity(this, 0, intent, PendingIntent.FLAG_IMMUTABLE))
      } else {
        @Suppress("DEPRECATION")
        startActivityAndCollapse(intent)
      }
    }
    if (isLocked) unlockAndRun(open) else open()
  }

  companion object {
    private const val TAG = "HotspotShareTile"
    private val main = Handler(Looper.getMainLooper())

    /** The tile while the system listens to it (the panel is open, or it was just tapped). */
    @Volatile private var listening: ShareTileService? = null

    /**
     * Redraws the tile with the current state. A tile the system listens to is drawn at once: some phones (Samsung)
     * ignore a request to listen to a tile that is already listened to, which left it showing the state of the tap.
     */
    fun update(context: Context) {
      listening?.let { tile -> main.post { tile.render() } }
      // Also when this app does not know of the tile: one added before it kept count tells it so when it listens.
      runCatching { requestListeningState(context, component(context)) }
    }

    private const val KEY_ADDED = "tile_added"

    private fun component(context: Context) = ComponentName(context, ShareTileService::class.java)

    private fun prefs(context: Context) = context.getSharedPreferences("settings", Context.MODE_PRIVATE)

    /** Whether the tile is in the Quick Settings panel, as far as the system has told this app. */
    fun added(context: Context): Boolean = prefs(context).getBoolean(KEY_ADDED, false)

    fun setAdded(context: Context, on: Boolean) {
      if (added(context) != on) prefs(context).edit().putBoolean(KEY_ADDED, on).apply()
    }

    /**
     * Makes sure the tile's service is on. An earlier build switched it off to take the tile out of the panel; Samsung
     * then kept the tile there, dead and untappable. Android has no call that removes a tile: only the user can.
     */
    fun ensureEnabled(context: Context) {
      val pm = context.packageManager
      if (pm.getComponentEnabledSetting(component(context)) == PackageManager.COMPONENT_ENABLED_STATE_DISABLED) {
        pm.setComponentEnabledSetting(component(context), PackageManager.COMPONENT_ENABLED_STATE_DEFAULT, PackageManager.DONT_KILL_APP)
      }
    }
  }
}
