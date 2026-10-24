package com.team27.mama.emergency.sms

import java.util.concurrent.ScheduledExecutorService
import java.util.concurrent.TimeUnit

fun interface Cancellable {
    fun cancel()
}

/** Runs a task after a delay; injected so the retry schedule is unit-tested on virtual time. */
fun interface SmsScheduler {
    fun schedule(delayMs: Long, task: () -> Unit): Cancellable
}

class ExecutorSmsScheduler(private val executor: ScheduledExecutorService) : SmsScheduler {
    override fun schedule(delayMs: Long, task: () -> Unit): Cancellable {
        val future = executor.schedule(task, delayMs, TimeUnit.MILLISECONDS)
        return Cancellable { future.cancel(false) }
    }
}
