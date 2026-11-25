package dev.hotspot.app

import android.content.Context
import android.graphics.BitmapFactory
import android.net.Uri
import androidx.core.content.FileProvider
import com.google.zxing.BarcodeFormat
import com.google.zxing.BinaryBitmap
import com.google.zxing.DecodeHintType
import com.google.zxing.RGBLuminanceSource
import com.google.zxing.common.GlobalHistogramBinarizer
import com.google.zxing.common.HybridBinarizer
import com.google.zxing.qrcode.QRCodeReader
import java.io.File

/**
 * QR codes read from a photo, for phones without Google Play services: their system QR scanner (which the app uses
 * otherwise) is not there, so the phone's own camera app takes a photo and the app finds the code in it. The app
 * itself never uses the camera and asks for no permission.
 */
object QrPhoto {
  /** Where the camera app puts the photo: in the app's cache, handed over through FileProvider ("scan/"). */
  fun uri(context: Context): Uri {
    val file = File(File(context.cacheDir, "scan").apply { mkdirs() }, "photo.jpg")
    return FileProvider.getUriForFile(context, "${context.packageName}.files", file)
  }

  /** The text of the QR code in the photo at [uri], or null when none is found. The photo is deleted afterwards. */
  fun decode(context: Context, uri: Uri): String? {
    try {
      val bounds = BitmapFactory.Options().apply { inJustDecodeBounds = true }
      context.contentResolver.openInputStream(uri)?.use { BitmapFactory.decodeStream(it, null, bounds) }
      if (bounds.outWidth <= 0 || bounds.outHeight <= 0) return null
      // A camera's photo has some 12 megapixels: a code that fills a good part of it is found at a fraction.
      var sample = 1
      while (maxOf(bounds.outWidth, bounds.outHeight) / (sample * 2) >= MAX_SIDE) sample *= 2
      val bitmap = context.contentResolver.openInputStream(uri)?.use {
        BitmapFactory.decodeStream(it, null, BitmapFactory.Options().apply { inSampleSize = sample })
      } ?: return null
      val pixels = IntArray(bitmap.width * bitmap.height)
      bitmap.getPixels(pixels, 0, bitmap.width, 0, 0, bitmap.width, bitmap.height)
      val (w, h) = bitmap.width to bitmap.height
      bitmap.recycle()
      return decodePixels(w, h, pixels)
    } catch (_: Exception) {
      return null
    } finally {
      File(File(context.cacheDir, "scan"), "photo.jpg").delete()
    }
  }

  /** The text of a QR code in an image of [width] x [height] ARGB [pixels], or null. */
  internal fun decodePixels(width: Int, height: Int, pixels: IntArray): String? {
    val source = RGBLuminanceSource(width, height, pixels)
    val hints = mapOf(
      DecodeHintType.TRY_HARDER to true,
      DecodeHintType.POSSIBLE_FORMATS to listOf(BarcodeFormat.QR_CODE),
    )
    for (binarized in listOf(BinaryBitmap(HybridBinarizer(source)), BinaryBitmap(GlobalHistogramBinarizer(source)))) {
      runCatching { return QRCodeReader().decode(binarized, hints).text }
    }
    return null
  }

  private const val MAX_SIDE = 1600
}
