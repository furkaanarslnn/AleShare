package dev.hotspot.app

import android.content.Intent
import android.net.VpnService
import android.os.Bundle
import androidx.activity.ComponentActivity
import androidx.activity.compose.setContent
import androidx.activity.enableEdgeToEdge
import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.Surface
import androidx.compose.ui.Modifier
import dev.hotspot.app.ui.MainScreen
import dev.hotspot.app.ui.theme.HotspotTheme

class MainActivity : ComponentActivity() {
  override fun onCreate(savedInstanceState: Bundle?) {
    super.onCreate(savedInstanceState)
    ShareTileService.ensureEnabled(this)

    enableEdgeToEdge()
    // A tapped join link starts the app; the screen asks the user about it, nothing happens before that.
    if (savedInstanceState == null) offerLink(intent)
    // A connection the user left on, whose process Android ended meanwhile, comes back when the app is opened.
    if (ClientState.state.value.phase == ClientPhase.Idle && ClientService.wanted(this) &&
      ClientStore.load(this) != null && VpnService.prepare(this) == null
    ) {
      ClientService.start(this)
    }
    setContent {
      HotspotTheme { Surface(modifier = Modifier.fillMaxSize(), color = MaterialTheme.colorScheme.background) { MainScreen() } }
    }
  }

  override fun onNewIntent(intent: Intent) {
    super.onNewIntent(intent)
    offerLink(intent)
  }

  private fun offerLink(intent: Intent?) {
    if (intent?.action == Intent.ACTION_VIEW) JoinInbox.offer(intent.dataString)
  }
}
