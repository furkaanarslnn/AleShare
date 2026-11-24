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
  internal const val CORE_ENTRY = "core"
  private const val IV_SIZE = 12
  private const val TRIES = 3
  private const val PAUSE_MILLIS = 250L
  private const val LOST = ".lost"

  /** Entries that are there but could not be decrypted in this run of the app. */
  private val unreadable: MutableSet<String> = java.util.Collections.synchronizedSet(mutableSetOf())

  /**
   * Whether [entry] holds something that could not be read. Its owner then does not write a fresh start over it
   * by itself: the keystore fails now and then for a while, and what it could not read today it may read at the
   * next start. (A fresh start written over it at once threw every pairing away for good.)
   */
  fun unreadable(entry: String): Boolean = entry in unreadable

  private fun existingKey(): SecretKey? {
    val ks = KeyStore.getInstance("AndroidKeyStore").apply { load(null) }
    return ks.getKey(KEY_ALIAS, null) as? SecretKey
  }

  /**
   * The key for writing: the one there is, or a new one when there is none. When something was saved before
   * ([hasEntries]) a missing key is asked for a few times first: a key made under the same name puts the old one,
   * and everything saved with it, out of reach for good, and the keystore may only be slow to answer.
   */
  private fun keyForWriting(hasEntries: Boolean): SecretKey {
    for (n in 0 until TRIES) {
      runCatching { existingKey() }.getOrNull()?.let { return it }
      if (!hasEntries) break
      Thread.sleep(PAUSE_MILLIS)
    }
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

  /** Reads with the key there is; a key is never made for reading. */
  private fun decrypt(stored: String): String {
    val raw = Base64.decode(stored, Base64.NO_WRAP)
    val key = existingKey() ?: throw IllegalStateException("the key is not in the keystore")
    val cipher = Cipher.getInstance("AES/GCM/NoPadding")
    cipher.init(Cipher.DECRYPT_MODE, key, GCMParameterSpec(128, raw, 0, IV_SIZE))
    return String(cipher.doFinal(raw, IV_SIZE, raw.size - IV_SIZE), Charsets.UTF_8)
  }

  /** Returns the saved text of [entry], or null if there is none or it cannot be decrypted (see [unreadable]). */
  fun get(context: Context, entry: String): String? {
    val stored = context.getSharedPreferences(PREFS, Context.MODE_PRIVATE).getString(entry, null) ?: return null
    // The keystore fails now and then for a moment (right after the phone started, or while it is busy): try
    // again before giving the entry up. A wrong key or a damaged entry stays wrong and is not tried again.
    var last: Exception? = null
    for (n in 0 until TRIES) {
      try {
        val text = decrypt(stored)
        unreadable.remove(entry)
        return text
      } catch (e: javax.crypto.AEADBadTagException) {
        last = e
        break
      } catch (e: IllegalArgumentException) { // not Base64, or too short: damaged
        last = e
        break
      } catch (e: Exception) {
        last = e
        if (n + 1 < TRIES) Thread.sleep(PAUSE_MILLIS)
      }
    }
    Log.w(TAG, "saved entry '$entry' is unreadable", last)
    unreadable.add(entry)
    return null
  }

  fun put(context: Context, entry: String, text: String) {
    val prefs = context.getSharedPreferences(PREFS, Context.MODE_PRIVATE)
    val cipher = Cipher.getInstance("AES/GCM/NoPadding")
    cipher.init(Cipher.ENCRYPT_MODE, keyForWriting(hasEntries = prefs.all.isNotEmpty()))
    val out = cipher.iv + cipher.doFinal(text.toByteArray(Charsets.UTF_8))
    val edit = prefs.edit()
    // What could not be read is put aside, not written over: should it read again one day, it can come back (see
    // [lost]).
    if (entry in unreadable) prefs.getString(entry, null)?.let { edit.putString(entry + LOST, it) }
    edit.putString(entry, Base64.encodeToString(out, Base64.NO_WRAP)).commit()
    unreadable.remove(entry)
  }

  fun remove(context: Context, entry: String) {
    context.getSharedPreferences(PREFS, Context.MODE_PRIVATE).edit().remove(entry).commit()
    unreadable.remove(entry)
  }

  /** What was put aside for [entry] when something was saved over an unreadable one, if it reads now. */
  fun lost(context: Context, entry: String): String? {
    val stored = context.getSharedPreferences(PREFS, Context.MODE_PRIVATE).getString(entry + LOST, null) ?: return null
    return runCatching { decrypt(stored) }.getOrNull()
  }

  fun dropLost(context: Context, entry: String) {
    context.getSharedPreferences(PREFS, Context.MODE_PRIVATE).edit().remove(entry + LOST).commit()
  }

  /** The core's state. */
  fun load(context: Context): String? = get(context, CORE_ENTRY)

  fun save(context: Context, json: String) = put(context, CORE_ENTRY, json)
}
