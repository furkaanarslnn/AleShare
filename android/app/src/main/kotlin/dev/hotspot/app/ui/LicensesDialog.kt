package dev.hotspot.app.ui

import androidx.compose.foundation.layout.Box
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.PaddingValues
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.lazy.LazyColumn
import androidx.compose.material3.CircularProgressIndicator
import androidx.compose.material3.HorizontalDivider
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.Surface
import androidx.compose.material3.Text
import androidx.compose.material3.TextButton
import androidx.compose.runtime.Composable
import androidx.compose.runtime.getValue
import androidx.compose.runtime.produceState
import androidx.compose.runtime.remember
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.platform.LocalContext
import androidx.compose.ui.unit.dp
import androidx.compose.ui.window.Dialog
import androidx.compose.ui.window.DialogProperties
import dev.hotspot.app.LicenseTexts
import dev.hotspot.app.parseLicenses
import kotlinx.coroutines.Dispatchers
import kotlinx.coroutines.withContext

/** A line of the license screen: the names a text covers, or a paragraph of a text. */
private sealed interface LicenseRow {
  data class Names(val names: List<String>) : LicenseRow
  data class Paragraph(val text: String, val intro: Boolean) : LicenseRow
}

/**
 * The license texts of everything the app is built from, on a screen of their own. They are long (the texts of
 * some two hundred components), so they are read off the main thread and shown paragraph by paragraph in a list
 * that only lays out what is on the screen.
 */
@Composable
fun LicensesDialog(onDismiss: () -> Unit) {
  val context = LocalContext.current
  val texts by produceState<LicenseTexts?>(initialValue = null) {
    value = withContext(Dispatchers.IO) {
      runCatching { parseLicenses(context.assets.open("licenses.txt").bufferedReader().use { it.readText() }) }
        .getOrDefault(LicenseTexts(listOf("Lisans metinleri okunamadı."), emptyList()))
    }
  }
  val rows = remember(texts) {
    texts?.let { t ->
      buildList {
        t.intro.forEach { add(LicenseRow.Paragraph(it, intro = true)) }
        t.sections.forEach { s ->
          add(LicenseRow.Names(s.names))
          s.paragraphs.forEach { add(LicenseRow.Paragraph(it, intro = false)) }
        }
      }
    }
  }
  Dialog(onDismissRequest = onDismiss, properties = DialogProperties(usePlatformDefaultWidth = false)) {
    Surface(modifier = Modifier.fillMaxSize(), color = MaterialTheme.colorScheme.background) {
      Column {
        Row(modifier = Modifier.fillMaxWidth().padding(start = 20.dp, end = 8.dp, top = 12.dp, bottom = 4.dp), verticalAlignment = Alignment.CenterVertically) {
          Text("Açık kaynak lisansları", modifier = Modifier.weight(1f), style = MaterialTheme.typography.titleLarge, color = MaterialTheme.colorScheme.onSurface)
          TextButton(onClick = onDismiss) { Text("Kapat") }
        }
        HorizontalDivider(color = MaterialTheme.colorScheme.outlineVariant)
        if (rows == null) {
          Box(modifier = Modifier.fillMaxSize(), contentAlignment = Alignment.Center) { CircularProgressIndicator() }
        } else {
          LazyColumn(contentPadding = PaddingValues(horizontal = 20.dp, vertical = 16.dp)) {
            items(rows.size) { i ->
              when (val row = rows[i]) {
                is LicenseRow.Names ->
                  Column(modifier = Modifier.padding(top = 20.dp, bottom = 10.dp)) {
                    HorizontalDivider(color = MaterialTheme.colorScheme.outlineVariant)
                    Text(
                      row.names.joinToString("\n"),
                      modifier = Modifier.padding(top = 14.dp),
                      style = MaterialTheme.typography.titleSmall,
                      color = MaterialTheme.colorScheme.primary,
                    )
                  }
                is LicenseRow.Paragraph ->
                  Text(
                    row.text,
                    modifier = Modifier.padding(bottom = 10.dp),
                    style = if (row.intro) MaterialTheme.typography.bodyMedium else MaterialTheme.typography.bodySmall,
                    color = if (row.intro) MaterialTheme.colorScheme.onSurface else MaterialTheme.colorScheme.onSurfaceVariant,
                  )
              }
            }
          }
        }
      }
    }
  }
}
