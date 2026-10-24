package com.team27.mama.trigger

import android.app.PendingIntent
import android.appwidget.AppWidgetManager
import android.appwidget.AppWidgetProvider
import android.content.Context
import android.widget.RemoteViews
import com.team27.mama.R
import com.team27.mama.contract.Trigger

/**
 * Home-screen widget: one tap starts [TriggerService] straight through a
 * foreground-service PendingIntent (a widget tap may start one from the
 * background; spike #10 checked it on the demo Xiaomi). It looks like the
 * app icon on purpose. Before setup is done the service ignores the tap.
 */
class TriggerWidgetProvider : AppWidgetProvider() {
    override fun onUpdate(context: Context, manager: AppWidgetManager, ids: IntArray) {
        val tap = PendingIntent.getForegroundService(
            context,
            0,
            TriggerService.intent(context, Trigger.WIDGET),
            PendingIntent.FLAG_IMMUTABLE or PendingIntent.FLAG_UPDATE_CURRENT,
        )
        val views = RemoteViews(context.packageName, R.layout.widget_trigger).apply {
            setOnClickPendingIntent(R.id.widget_trigger, tap)
        }
        manager.updateAppWidget(ids, views)
        TriggerLog.log(context, "widget update ids=${ids.joinToString()}")
    }
}
