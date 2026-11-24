package dev.hotspot.app

import android.app.PendingIntent
import android.appwidget.AppWidgetManager
import android.appwidget.AppWidgetProvider
import android.content.ComponentName
import android.content.Context
import android.content.Intent
import android.widget.RemoteViews

/**
 * The home screen widget. One tap starts sharing, another stops it; it shows whether sharing is on and how many
 * devices use it (see [ShareControls]). When a tap could not start sharing it opens the app instead.
 */
class ShareWidget : AppWidgetProvider() {
  override fun onUpdate(context: Context, manager: AppWidgetManager, ids: IntArray) = render(context, manager, ids)

  companion object {
    /** Draws every placed widget again. */
    fun update(context: Context) {
      val manager = AppWidgetManager.getInstance(context) ?: return
      val ids = manager.getAppWidgetIds(ComponentName(context, ShareWidget::class.java))
      if (ids.isNotEmpty()) render(context, manager, ids)
    }

    private fun render(context: Context, manager: AppWidgetManager, ids: IntArray) {
      val s = ShareControls.status(context)
      val tap = when (s.action) {
        ShareAction.Toggle -> PendingIntent.getForegroundService(
          context, 0, Intent(context, GatewayService::class.java).setAction(GatewayService.ACTION_TOGGLE),
          PendingIntent.FLAG_IMMUTABLE or PendingIntent.FLAG_UPDATE_CURRENT,
        )
        ShareAction.OpenApp -> PendingIntent.getActivity(
          context, 0, Intent(context, MainActivity::class.java).addFlags(Intent.FLAG_ACTIVITY_NEW_TASK), PendingIntent.FLAG_IMMUTABLE,
        )
      }
      val views = RemoteViews(context.packageName, if (s.on) R.layout.widget_on else R.layout.widget_off).apply {
        setTextViewText(R.id.widget_title, s.title)
        setTextViewText(R.id.widget_status, s.detail)
        setContentDescription(android.R.id.background, "${s.title}. ${s.detail}")
        setOnClickPendingIntent(android.R.id.background, tap)
      }
      manager.updateAppWidget(ids, views)
    }
  }
}
