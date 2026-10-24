// Package ratelimit caps how often the server reaches one phone number, and
// all numbers together, over a sliding window. The call engine counts the
// calls it places and the notifier the family messages it sends: there is
// no auth on /devices or /incidents, so without a cap anyone could make the
// server place real calls and send real messages in a loop, each one paid
// for. The counts live in memory, which is enough for the one Render
// instance; a restart resets them.
package ratelimit

import (
	"fmt"
	"sync"
	"time"
)

// Limiter allows at most PerNumber events for one number, and Total for all
// numbers together, within any Window. A refused event is not counted, so a
// flood that keeps hitting a cap does not push its end further away. Create
// it with a struct literal; it is safe for concurrent use. A PerNumber or
// Total of zero or less leaves that cap off, and a nil *Limiter allows
// everything.
type Limiter struct {
	PerNumber int
	Total     int
	Window    time.Duration
	Now       func() time.Time // time.Now when nil

	mu     sync.Mutex
	events []event        // every counted event still in the window, oldest first
	counts map[string]int // number -> how many of events are its
}

type event struct {
	at     time.Time
	number string
}

// Exceeded is the error of a refused event: the cap it hit. It never holds
// the number, so it is safe to log.
type Exceeded struct {
	Total  bool // the Total cap; false: PerNumber
	Max    int
	Window time.Duration
}

func (e *Exceeded) Error() string {
	if e.Total {
		return fmt.Sprintf("over the limit of %d in total per %s", e.Max, e.Window)
	}
	return fmt.Sprintf("over the limit of %d per number per %s", e.Max, e.Window)
}

// Allow counts one event for number and returns nil when it fits both caps.
// Otherwise it counts nothing and returns an *Exceeded.
func (l *Limiter) Allow(number string) error {
	if l == nil {
		return nil
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	now := time.Now()
	if l.Now != nil {
		now = l.Now()
	}
	l.expire(now)
	switch {
	case l.PerNumber > 0 && l.counts[number] >= l.PerNumber:
		return &Exceeded{Max: l.PerNumber, Window: l.Window}
	case l.Total > 0 && len(l.events) >= l.Total:
		return &Exceeded{Total: true, Max: l.Total, Window: l.Window}
	}
	if l.counts == nil {
		l.counts = map[string]int{}
	}
	l.events = append(l.events, event{at: now, number: number})
	l.counts[number]++
	return nil
}

// expire forgets the events that left the window. events is oldest first,
// so they are a prefix of it, and each one tells which number to uncount:
// the work is per expired event, however many numbers there are.
func (l *Limiter) expire(now time.Time) {
	n := 0
	for n < len(l.events) && now.Sub(l.events[n].at) >= l.Window {
		number := l.events[n].number
		if l.counts[number]--; l.counts[number] <= 0 {
			delete(l.counts, number)
		}
		n++
	}
	l.events = append(l.events[:0], l.events[n:]...)
}

// String describes the caps for the boot log, e.g. "6 per number and 40 in
// total per 10m0s".
func (l *Limiter) String() string {
	return fmt.Sprintf("%d per number and %d in total per %s", l.PerNumber, l.Total, l.Window)
}
