package dev.hotspot.app

import android.content.Context
import dev.hotspot.wgcore.Core

/** One Go core per process, restored from the encrypted store. */
object CoreHolder {
  private var core: Core? = null

  @Synchronized
  fun get(context: Context): Core {
    core?.let { return it }
    val app = context.applicationContext
    var saved = SecureStore.load(app)
    // A state that could not be read on an earlier day was put aside when something had to be saved over it. If
    // it reads now and no device was paired since, it comes back, and every pairing with it.
    SecureStore.lost(app, SecureStore.CORE_ENTRY)?.let { lost ->
      if (!hasPeers(saved) && !SecureStore.unreadable(SecureStore.CORE_ENTRY)) {
        SecureStore.save(app, lost)
        saved = lost
      }
      SecureStore.dropLost(app, SecureStore.CORE_ENTRY)
    }
    val created = Core(saved ?: "")
    // Nothing was saved yet: keep the new state. A saved state that could not be read stays where it is, though:
    // this run's empty state does not replace it by itself (see SecureStore.unreadable).
    if (saved == null && !SecureStore.unreadable(SecureStore.CORE_ENTRY)) SecureStore.save(app, created.stateJSON())
    core = created
    return created
  }

  /** Whether a saved state lists any paired device. */
  private fun hasPeers(state: String?): Boolean =
    state != null && runCatching { (org.json.JSONObject(state).optJSONArray("peers")?.length() ?: 0) > 0 }.getOrDefault(false)

  /** The core if it was already created; never creates one. */
  @Synchronized
  fun peek(): Core? = core

  fun save(context: Context, core: Core) = SecureStore.save(context.applicationContext, core.stateJSON())
}
