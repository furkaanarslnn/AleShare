package dev.hotspot.app

import android.content.Context
import android.content.pm.PackageManager
import android.os.Build
import android.telephony.TelephonyManager

/**
 * Whether the device has a SIM card (or an eSIM) whose mobile data it could share. A tablet without one, or a phone
 * with its SIM taken out, has no mobile data to share: with mobile data as the source, "Paylaşımı başlat", the Quick
 * Settings tile and the widget are off there. It may still share its Wi-Fi (see Source), or use another phone's
 * internet from Bağlan. Reading the SIM's state needs no permission.
 */
object SimCard {
  fun present(context: Context): Boolean {
    if (!context.packageManager.hasSystemFeature(PackageManager.FEATURE_TELEPHONY)) return false
    val phone = context.getSystemService(TelephonyManager::class.java) ?: return false
    return runCatching {
      @Suppress("DEPRECATION")
      val slots = if (Build.VERSION.SDK_INT >= 30) phone.activeModemCount else phone.phoneCount
      usable((0 until slots).map { phone.getSimState(it) })
    }.getOrDefault(true) // no way to look: as before, let the user try
  }

  /**
   * Whether any of the slots' [states] may hold a SIM: one that is missing or broken in every slot counts as none.
   * A SIM that waits for its PIN, or one Android has not read yet (right after a restart, in airplane mode), counts
   * as there, so sharing is never shut off by a passing state.
   */
  internal fun usable(states: List<Int>): Boolean = states.any { it !in NONE }

  private val NONE = setOf(
    TelephonyManager.SIM_STATE_ABSENT,
    TelephonyManager.SIM_STATE_CARD_IO_ERROR,
    TelephonyManager.SIM_STATE_CARD_RESTRICTED,
    TelephonyManager.SIM_STATE_PERM_DISABLED,
  )

  /** What the app, the tile and the widget say when there is no SIM card. */
  const val MISSING = "Bu cihazda SIM kart yok: paylaşılacak mobil veri olmadığı için mobil veri paylaşılamaz. " +
    "Kaynak olarak Wi-Fi'yi seçerek telefonun bağlı olduğu Wi-Fi'yi paylaşabilirsin."
}
