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
    val saved = SecureStore.load(app)
    val created = Core(saved ?: "")
    if (saved == null) SecureStore.save(app, created.stateJSON())
    core = created
    return created
  }

  /** The core if it was already created; never creates one. */
  @Synchronized
  fun peek(): Core? = core

  fun save(context: Context, core: Core) = SecureStore.save(context.applicationContext, core.stateJSON())
}
