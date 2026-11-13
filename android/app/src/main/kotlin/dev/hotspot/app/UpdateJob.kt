package dev.hotspot.app

import android.app.job.JobInfo
import android.app.job.JobParameters
import android.app.job.JobScheduler
import android.app.job.JobService
import android.content.ComponentName
import android.content.Context

/**
 * Looks for a newer release on GitHub while the app is closed, so that its user hears of one without having to
 * open the app. Android runs this job about every six hours when the device has a network; the look itself is
 * [OnlineUpdate.check], a few kilobytes, and it tells the user once about each release. With "Yeni sürümleri
 * denetle" switched off there is no job.
 *
 * It is a best effort: Android puts jobs of rarely used apps off, sometimes for a day. Opening the app and sharing
 * look too.
 */
class UpdateJob : JobService() {
  override fun onStartJob(params: JobParameters): Boolean {
    OnlineUpdate.check(this) { jobFinished(params, false) }
    return true // the look goes on on its own thread
  }

  override fun onStopJob(params: JobParameters): Boolean = false

  companion object {
    private const val ID = 4101
    private const val EVERY_MS = 6 * 3_600_000L

    /** Sets the job up if it is wanted and not there yet, and takes it away if it is not wanted. */
    fun schedule(context: Context) {
      val scheduler = context.getSystemService(JobScheduler::class.java) ?: return
      if (!OnlineUpdate.enabled(context)) {
        scheduler.cancel(ID)
        return
      }
      if (scheduler.getPendingJob(ID) != null) return
      val job = JobInfo.Builder(ID, ComponentName(context, UpdateJob::class.java))
        .setPeriodic(EVERY_MS)
        .setRequiredNetworkType(JobInfo.NETWORK_TYPE_ANY)
        .setPersisted(true) // across restarts of the device
        .build()
      runCatching { scheduler.schedule(job) }
    }
  }
}
