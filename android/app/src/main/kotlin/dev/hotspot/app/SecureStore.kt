package dev.hotspot.app

import android.content.Context
import android.security.keystore.KeyGenParameterSpec
import android.security.keystore.KeyProperties
import android.util.Base64
import android.util.Log
import java.security.KeyStore
import javax.crypto.Cipher
import javax.crypto.KeyGenerator
import javax.crypto.SecretKey
import javax.crypto.spec.GCMParameterSpec

/**
 * Keeps small secrets (the core's state, the Wi-Fi password) encrypted with an
 * AES-GCM key that never leaves the Android Keystore.
 */
object SecureStore {
  private const val TAG = "SecureStore"
  private const val KEY_ALIAS = "hotspot_state_key"
  private const val PREFS = "state"
  private const val CORE_ENTRY = "core"
  private const val IV_SIZE = 12

  private fun key(): SecretKey {
    val ks = KeyStore.getInstance("AndroidKeyStore").apply { load(null) }
    (ks.getKey(KEY_ALIAS, null) as? SecretKey)?.let { return it }
    val gen = KeyGenerator.getInstance(KeyProperties.KEY_ALGORITHM_AES, "AndroidKeyStore")
    gen.init(
      KeyGenParameterSpec.Builder(KEY_ALIAS, KeyProperties.PURPOSE_ENCRYPT or KeyProperties.PURPOSE_DECRYPT)
        .setBlockModes(KeyProperties.BLOCK_MODE_GCM)
        .setEncryptionPaddings(KeyProperties.ENCRYPTION_PADDING_NONE)
        .setKeySize(256)
        .build()
    )
    return gen.generateKey()
  }

  /** Returns the saved text of [entry], or null if there is none or it can no longer be decrypted. */
  fun get(context: Context, entry: String): String? {
    val stored = context.getSharedPreferences(PREFS, Context.MODE_PRIVATE).getString(entry, null) ?: return null
    return try {
      val raw = Base64.decode(stored, Base64.NO_WRAP)
      val cipher = Cipher.getInstance("AES/GCM/NoPadding")
      cipher.init(Cipher.DECRYPT_MODE, key(), GCMParameterSpec(128, raw, 0, IV_SIZE))
      String(cipher.doFinal(raw, IV_SIZE, raw.size - IV_SIZE), Charsets.UTF_8)
    } catch (e: Exception) {
      Log.w(TAG, "saved entry '$entry' is unreadable, starting afresh", e)
      null
    }
  }

  fun put(context: Context, entry: String, text: String) {
    val cipher = Cipher.getInstance("AES/GCM/NoPadding")
    cipher.init(Cipher.ENCRYPT_MODE, key())
    val out = cipher.iv + cipher.doFinal(text.toByteArray(Charsets.UTF_8))
    context.getSharedPreferences(PREFS, Context.MODE_PRIVATE)
      .edit()
      .putString(entry, Base64.encodeToString(out, Base64.NO_WRAP))
      .commit()
  }

  fun remove(context: Context, entry: String) {
    context.getSharedPreferences(PREFS, Context.MODE_PRIVATE).edit().remove(entry).commit()
  }

  /** The core's state. */
  fun load(context: Context): String? = get(context, CORE_ENTRY)

  fun save(context: Context, json: String) = put(context, CORE_ENTRY, json)
}
