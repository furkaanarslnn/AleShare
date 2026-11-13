package dev.hotspot.app

/** One license text and the components it covers. */
data class LicenseSection(val names: List<String>, val paragraphs: List<String>)

/** assets/licenses.txt, read: what stands before the first section, then the sections. */
data class LicenseTexts(val intro: List<String>, val sections: List<LicenseSection>)

/** The line that frames the names of a section (the notices command in core/cmd/notices writes the file). */
private val RULE = "=".repeat(80)

/**
 * Reads the license texts of what the app is built from. A section is a framed list of names followed by the
 * text they share:
 *
 *     ====…====
 *     name
 *     another name
 *     ====…====
 *
 *     the text
 */
fun parseLicenses(text: String): LicenseTexts {
  val intro = mutableListOf<String>()
  val sections = mutableListOf<LicenseSection>()
  var names: MutableList<String>? = null // the names of the section being read; null before the first one
  var inNames = false
  val body = mutableListOf<String>()
  fun close() {
    val paragraphs = paragraphsOf(body)
    names?.let { sections += LicenseSection(it, paragraphs) } ?: intro.addAll(paragraphs)
    body.clear()
  }
  for (line in text.replace("\r\n", "\n").split('\n')) {
    when {
      line == RULE && !inNames -> { close(); names = mutableListOf(); inNames = true }
      line == RULE -> inNames = false
      inNames -> names?.add(line.trim())
      else -> body += line
    }
  }
  close()
  return LicenseTexts(intro, sections)
}

private fun paragraphsOf(lines: List<String>): List<String> {
  val out = mutableListOf<String>()
  val paragraph = mutableListOf<String>()
  for (line in lines + "") {
    if (line.isBlank()) {
      if (paragraph.isNotEmpty()) out += reflow(paragraph)
      paragraph.clear()
    } else {
      paragraph += line
    }
  }
  return out
}

private val LIST_ITEM = Regex("""^(\*|-|•|\d+[.)]|\([a-z0-9]{1,3}\))\s""")
private val DIVIDER = Regex("""^[-=*_#~.]{4,}$""")

/**
 * License texts are wrapped by hand at some eighty letters, which a phone's screen breaks once more, in the
 * middle of every line. This joins the lines that were wrapped (the line before them is a long one), so that the
 * screen wraps the paragraph itself; short lines, list items and dividers keep their own lines.
 */
fun reflow(lines: List<String>): String {
  val out = StringBuilder()
  var before = ""
  for (raw in lines) {
    val line = raw.trim()
    if (line.isEmpty()) continue
    if (out.isNotEmpty()) {
      val wrapped = before.length >= 55 && !LIST_ITEM.containsMatchIn(line) && !DIVIDER.matches(line) && !DIVIDER.matches(before)
      out.append(if (wrapped) ' ' else '\n')
    }
    out.append(line)
    before = line
  }
  return out.toString()
}
