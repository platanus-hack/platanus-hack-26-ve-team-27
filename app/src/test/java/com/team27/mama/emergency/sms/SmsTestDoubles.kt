package com.team27.mama.emergency.sms

/** Virtual time: tasks run only when [advanceBy] passes their due time. */
class FakeScheduler : SmsScheduler {
    private class Task(val dueAt: Long, val run: () -> Unit, var cancelled: Boolean = false)

    var now = 0L
        private set
    private val tasks = mutableListOf<Task>()

    /** Delays asked for, in order, including ones later cancelled. */
    val requestedDelays = mutableListOf<Long>()

    val pendingCount: Int get() = tasks.count { !it.cancelled }

    override fun schedule(delayMs: Long, task: () -> Unit): Cancellable {
        requestedDelays += delayMs
        val t = Task(now + delayMs, task)
        tasks += t
        return Cancellable { t.cancelled = true }
    }

    fun advanceBy(ms: Long) {
        val until = now + ms
        while (true) {
            val next = tasks.filter { !it.cancelled && it.dueAt <= until }.minByOrNull { it.dueAt } ?: break
            tasks -= next
            now = next.dueAt
            next.run()
        }
        now = until
    }
}

/** Records each send with the virtual time and answers [results] in order (true when it runs out). */
class FakeSender(private val scheduler: FakeScheduler, private val results: List<Boolean> = emptyList()) : SmsSender {
    data class Sent(val at: Long, val number: String, val text: String)

    val sent = mutableListOf<Sent>()

    override fun send(number: String, text: String, onResult: (Boolean) -> Unit) {
        sent += Sent(scheduler.now, number, text)
        onResult(results.getOrElse(sent.size - 1) { true })
    }
}
