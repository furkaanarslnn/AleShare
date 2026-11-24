package dev.hotspot.app.ui

import androidx.compose.ui.graphics.Color
import androidx.compose.ui.graphics.SolidColor
import androidx.compose.ui.graphics.StrokeCap
import androidx.compose.ui.graphics.StrokeJoin
import androidx.compose.ui.graphics.vector.ImageVector
import androidx.compose.ui.graphics.vector.addPathNodes
import androidx.compose.ui.unit.dp

/** A small outline icon set on a 24 x 24 grid, so every icon has the same weight. Drawn black; tint at the use site. */
object AppIcons {
  private fun outline(name: String, vararg paths: String): ImageVector =
    ImageVector.Builder(name, 24.dp, 24.dp, 24f, 24f)
      .apply {
        for (d in paths) {
          addPath(
            pathData = addPathNodes(d),
            stroke = SolidColor(Color.Black),
            strokeLineWidth = 1.75f,
            strokeLineCap = StrokeCap.Round,
            strokeLineJoin = StrokeJoin.Round,
          )
        }
      }
      .build()

  val Shield = outline("shield", "M12 3l8 3v5.5c0 4.8-3.3 8.3-8 9.5-4.7-1.2-8-4.7-8-9.5V6z", "M9 12.2l2.2 2.2L15.2 10.4")

  val Copy =
    outline(
      "copy",
      "M9 8.5h10a1 1 0 0 1 1 1V19a1 1 0 0 1-1 1H9a1 1 0 0 1-1-1V9.5a1 1 0 0 1 1-1z",
      "M16 5.5V5a1 1 0 0 0-1-1H5a1 1 0 0 0-1 1v10a1 1 0 0 0 1 1h.5",
    )

  val Eye = outline("eye", "M2 12s3.5-7 10-7 10 7 10 7-3.5 7-10 7S2 12 2 12z", "M12 9.2a2.8 2.8 0 1 0 0 5.6 2.8 2.8 0 0 0 0-5.6z")

  val EyeOff =
    outline(
      "eye_off",
      "M2 12s3.5-7 10-7c1.6 0 3 .4 4.3 1M22 12s-3.5 7-10 7c-1.6 0-3-.4-4.3-1",
      "M9.9 9.9a2.8 2.8 0 0 0 4.2 3.7",
      "M4 4l16 16",
    )

  val Trash = outline("trash", "M4 7h16", "M10 11v6M14 11v6", "M6 7l1 12a1 1 0 0 0 1 1h8a1 1 0 0 0 1-1l1-12", "M9 7V4h6v3")

  val Plus = outline("plus", "M12 5v14M5 12h14")

  val Qr = outline("qr", "M4 4h6v6H4z", "M14 4h6v6h-6z", "M4 14h6v6H4z", "M14 14h2v2h-2z", "M18 14h2", "M14 18h2", "M18 18h2v2h-2z")

  val ChevronDown = outline("chevron_down", "M6 9l6 6 6-6")

  val ArrowUp = outline("arrow_up", "M12 19V5M6 11l6-6 6 6")

  val ArrowDown = outline("arrow_down", "M12 5v14M6 13l6 6 6-6")

  val Link = outline("link", "M10 14a4 4 0 0 0 5.7 0l3-3a4 4 0 0 0-5.7-5.7l-1 1", "M14 10a4 4 0 0 0-5.7 0l-3 3a4 4 0 0 0 5.7 5.7l1-1")

  val Laptop = outline("laptop", "M5 6a1 1 0 0 1 1-1h12a1 1 0 0 1 1 1v9H5z", "M3 19h18")

  val Phone = outline("phone", "M8 3h8a1 1 0 0 1 1 1v16a1 1 0 0 1-1 1H8a1 1 0 0 1-1-1V4a1 1 0 0 1 1-1z", "M11 18h2")

  val Wifi = outline("wifi", "M2 9a15 15 0 0 1 20 0", "M5 12.5a10 10 0 0 1 14 0", "M8.5 16a5 5 0 0 1 7 0", "M12 19.5h.01")

  val Alert = outline("alert", "M12 3a9 9 0 1 0 0 18 9 9 0 0 0 0-18z", "M12 8v5", "M12 16.5h.01")

  val Toggle = outline("toggle", "M8 7h8a5 5 0 0 1 0 10H8A5 5 0 0 1 8 7z", "M16 9.6a2.4 2.4 0 1 0 0 4.8 2.4 2.4 0 0 0 0-4.8z")

  val Edit = outline("edit", "M4 20h4L19 9a2.8 2.8 0 0 0-4-4L4 16z", "M13.5 6.5l4 4")

  val Download = outline("download", "M12 4v11", "M7 10l5 5 5-5", "M5 20h14")

  val Clock = outline("clock", "M12 3a9 9 0 1 0 0 18 9 9 0 0 0 0-18z", "M12 7.5V12l3 2")

  val Chart = outline("chart", "M4 20h16", "M7 16v-4", "M12 16V6", "M17 16v-7")

  val Sliders = outline("sliders", "M4 7h8", "M16 7h4", "M14 5v4", "M4 17h3", "M11 17h9", "M9 15v4")
}
