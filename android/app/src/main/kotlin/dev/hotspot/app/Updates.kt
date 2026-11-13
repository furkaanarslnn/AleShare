package dev.hotspot.app

import android.content.Context
import android.content.pm.PackageInfo
import android.content.pm.PackageManager
import android.os.Build
import androidx.core.content.pm.PackageInfoCompat
import java.io.File

/**
 * The releases a sharing phone keeps for the paired devices (see Core.SetAppUpdate): this app's own APK or a newer
 * one a device brought, and the Windows program that comes inside the app (assets/hotspot-pc.exe) or a newer one a
 * computer brought. Whichever device has the newest release, the others get it through the phone.
 */
object Updates {
  /** One APK and its release. */
  data class Apk(val path: String, val code: Long, val name: String)

  private const val BUNDLED = "hotspot-pc.exe"

  /** Where the releases are kept; FileProvider hands APKs from here to Android's installer. */
  fun storeDir(context: Context): File = File(context.filesDir, "store").apply { mkdirs() }

  /**
   * The token paired devices use with this phone's store (see Core.setUpdateToken): kept across the app's restarts,
   * so that a computer's window goes on reading the phone after an update; new when a device is removed, so that a
   * removed one cannot go on with the old one.
   */
  fun token(context: Context): String =
    prefs(context).getString(KEY_TOKEN, null)?.takeIf { validToken(it) } ?: newToken(context)

  /** Gives the store a new token, after a device was removed. */
  fun renewToken(context: Context, core: dev.hotspot.wgcore.Core?) {
    val t = newToken(context)
    runCatching { core?.setUpdateToken(t) }
  }

  private fun newToken(context: Context): String {
    val bytes = ByteArray(16).also { java.security.SecureRandom().nextBytes(it) }
    val t = bytes.joinToString("") { "%02x".format(it) }
    prefs(context).edit().putString(KEY_TOKEN, t).apply()
    return t
  }

  private fun prefs(context: Context) = context.getSharedPreferences("settings", Context.MODE_PRIVATE)

  private const val KEY_TOKEN = "store_token"

  /** The Windows program that came inside this app, copied out once per release; null when this build has none. */
  fun bundledProgram(context: Context): File? {
    val out = File(storeDir(context), "bundled-pc.exe")
    val mark = File(storeDir(context), "bundled-pc.release")
    val release = ownVersionCode(context).toString()
    if (out.exists() && mark.takeIf { it.exists() }?.readText() == release) return out
    return runCatching {
      context.assets.open(BUNDLED).use { input -> out.outputStream().use { input.copyTo(it) } }
      mark.writeText(release)
      out
    }.getOrNull()
  }

  /** The newest APK to offer: a newer one a device brought, when it is this app signed with this app's key, or our own. */
  fun bestApk(context: Context): Apk {
    val own = ownVersionCode(context)
    var best = Apk(context.applicationInfo.sourceDir, own, versionName(context))
    storeDir(context).listFiles { f -> f.name.startsWith("received-") && f.name.endsWith(".apk") }?.forEach { f ->
      val checked = check(context, f, claimed = null)
      when {
        checked == null || checked.code <= own -> f.delete() // installed by now, or not ours
        checked.code > best.code -> best = checked
      }
    }
    return best
  }

  /**
   * Checks an APK a device brought: this app, signed with the same key as the installed one, and (when [claimed]
   * is given) the release the device said. Returns it, or null.
   */
  fun check(context: Context, file: File, claimed: Long?): Apk? {
    val pm = context.packageManager
    val theirs = archiveInfo(pm, file) ?: return null
    if (theirs.packageName != context.packageName) return null
    val code = PackageInfoCompat.getLongVersionCode(theirs)
    if (claimed != null && code != claimed) return null
    val mine = installedInfo(pm, context.packageName) ?: return null
    if (signers(theirs).isEmpty() || signers(theirs) != signers(mine)) return null
    return Apk(file.path, code, theirs.versionName ?: releaseName(code))
  }

  private fun archiveInfo(pm: PackageManager, file: File): PackageInfo? =
    if (Build.VERSION.SDK_INT >= 28) pm.getPackageArchiveInfo(file.path, PackageManager.GET_SIGNING_CERTIFICATES)
    else @Suppress("DEPRECATION") pm.getPackageArchiveInfo(file.path, PackageManager.GET_SIGNATURES)

  private fun installedInfo(pm: PackageManager, name: String): PackageInfo? = runCatching {
    if (Build.VERSION.SDK_INT >= 28) pm.getPackageInfo(name, PackageManager.GET_SIGNING_CERTIFICATES)
    else @Suppress("DEPRECATION") pm.getPackageInfo(name, PackageManager.GET_SIGNATURES)
  }.getOrNull()

  private fun signers(info: PackageInfo): Set<String> {
    val sigs = if (Build.VERSION.SDK_INT >= 28) info.signingInfo?.apkContentsSigners else @Suppress("DEPRECATION") info.signatures
    return sigs.orEmpty().map { it.toCharsString() }.toSet()
  }

  private fun versionName(context: Context): String =
    runCatching { context.packageManager.getPackageInfo(context.packageName, 0).versionName }.getOrNull() ?: ""
}

/** A store token as the core takes it: 32 hex digits. */
internal fun validToken(t: String): Boolean = t.length == 32 && t.all { it in '0'..'9' || it in 'a'..'f' }
