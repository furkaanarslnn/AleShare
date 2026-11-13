package dev.hotspot.app

import android.content.Context
import dev.hotspot.wgcore.Core
import dev.hotspot.wgcore.PairingListener
import java.util.concurrent.CompletableFuture
import java.util.concurrent.TimeUnit
import kotlinx.coroutines.flow.MutableStateFlow
import kotlinx.coroutines.flow.StateFlow
import kotlinx.coroutines.flow.asStateFlow

/** A computer asking to join. The user compares [sas] with the number on the computer's screen. */
class PairingRequest(val name: String, val sas: String, private val answer: CompletableFuture<Boolean>) {
  fun respond(accept: Boolean) {
    answer.complete(accept)
  }
}

/** Connects the Go core's pairing window to the UI. */
object PairingBridge : PairingListener {
  private const val ANSWER_TIMEOUT_SECONDS = 110L

  private val _request = MutableStateFlow<PairingRequest?>(null)
  val request: StateFlow<PairingRequest?> = _request.asStateFlow()

  /** The name of the computer paired last, for the screen to say so; null until one pairs. */
  private val _paired = MutableStateFlow<Paired?>(null)
  val pairedEvents: StateFlow<Paired?> = _paired.asStateFlow()

  /** One pairing that happened; [at] tells two pairings of the same name apart. */
  data class Paired(val name: String, val at: Long)

  @Volatile
  private var appContext: Context? = null

  /** Opens the pairing window (three minutes, one pairing). */
  fun start(context: Context, core: Core) {
    appContext = context.applicationContext
    core.startPairing(this)
  }

  /** Called on a Go thread; blocks until the user answered, or refuses after a timeout. */
  override fun approve(name: String, sas: String): Boolean {
    val answer = CompletableFuture<Boolean>()
    _request.value = PairingRequest(name, sas, answer)
    return try {
      answer.get(ANSWER_TIMEOUT_SECONDS, TimeUnit.SECONDS)
    } catch (_: Exception) {
      false
    } finally {
      _request.value = null
    }
  }

  override fun paired(name: String) {
    _paired.value = Paired(name, System.nanoTime())
    val context = appContext ?: return
    CoreHolder.peek()?.let { CoreHolder.save(context, it) }
  }
}
