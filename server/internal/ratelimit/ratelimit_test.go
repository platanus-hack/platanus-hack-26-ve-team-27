package ratelimit

import (
	"errors"
	"fmt"
	"strings"
	"sync"
	"testing"
	"time"
)

type fakeClock struct {
	mu sync.Mutex
	t  time.Time
}

func (c *fakeClock) now() time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.t
}

func (c *fakeClock) advance(d time.Duration) {
	c.mu.Lock()
	c.t = c.t.Add(d)
	c.mu.Unlock()
}

const (
	victim = "+584141234567"
	other  = "+584241112233"
)

func newLimiter(perNumber, total int) (*Limiter, *fakeClock) {
	clock := &fakeClock{t: time.Date(2026, 10, 24, 9, 10, 0, 0, time.UTC)}
	return &Limiter{PerNumber: perNumber, Total: total, Window: 10 * time.Minute, Now: clock.now}, clock
}

// allowN calls Allow n times for number and returns how many it allowed.
func allowN(l *Limiter, number string, n int) int {
	ok := 0
	for range n {
		if l.Allow(number) == nil {
			ok++
		}
	}
	return ok
}

func TestPerNumberCapRefusesOnlyThatNumber(t *testing.T) {
	l, _ := newLimiter(3, 100)

	if got := allowN(l, victim, 10); got != 3 {
		t.Errorf("allowed %d calls to one number, want 3", got)
	}
	var ex *Exceeded
	if err := l.Allow(victim); !errors.As(err, &ex) || ex.Total || ex.Max != 3 {
		t.Errorf("Allow over the cap = %v, want a per-number *Exceeded of 3", err)
	}
	if err := l.Allow(other); err != nil {
		t.Errorf("another number refused: %v", err)
	}
}

func TestTotalCapRefusesEveryNumber(t *testing.T) {
	l, _ := newLimiter(3, 5)

	ok := 0
	for i := range 20 {
		if l.Allow(fmt.Sprintf("+5841400000%02d", i)) == nil {
			ok++
		}
	}
	if ok != 5 {
		t.Errorf("allowed %d over many numbers, want the total cap 5", ok)
	}
	var ex *Exceeded
	if err := l.Allow("+584149999999"); !errors.As(err, &ex) || !ex.Total || ex.Max != 5 {
		t.Errorf("Allow over the total = %v, want a total *Exceeded of 5", err)
	}
}

func TestWindowSlides(t *testing.T) {
	l, clock := newLimiter(2, 100)

	l.Allow(victim) // t=0
	clock.advance(4 * time.Minute)
	l.Allow(victim) // t=4m
	if l.Allow(victim) == nil {
		t.Fatal("third call within the window allowed")
	}
	// At t=9m59s both are still in the window.
	clock.advance(5*time.Minute + 59*time.Second)
	if l.Allow(victim) == nil {
		t.Fatal("allowed before the first call left the window")
	}
	// At t=10m the first call leaves: one slot, not two.
	clock.advance(time.Second)
	if got := allowN(l, victim, 3); got != 1 {
		t.Errorf("allowed %d at t=10m, want 1 (the t=4m call still counts)", got)
	}
	// At t=14m the t=4m call leaves too.
	clock.advance(4 * time.Minute)
	if got := allowN(l, victim, 3); got != 1 {
		t.Errorf("allowed %d at t=14m, want 1 (the t=10m call still counts)", got)
	}
}

func TestRefusedEventsAreNotCounted(t *testing.T) {
	l, clock := newLimiter(1, 100)

	l.Allow(victim)
	// A flood of refused calls must not keep the number blocked.
	for range 5 {
		clock.advance(time.Minute)
		l.Allow(victim)
	}
	clock.advance(5 * time.Minute) // 10 min after the only counted call
	if err := l.Allow(victim); err != nil {
		t.Errorf("still refused after the window: %v", err)
	}
}

func TestExpiredNumbersAreForgotten(t *testing.T) {
	l, clock := newLimiter(5, 0)

	for i := range 1000 {
		l.Allow(fmt.Sprintf("+58414%07d", i))
	}
	clock.advance(10 * time.Minute)
	l.Allow(victim)
	l.mu.Lock()
	defer l.mu.Unlock()
	if len(l.events) != 1 || len(l.counts) != 1 {
		t.Errorf("after the window: %d events and %d numbers kept, want 1 and 1", len(l.events), len(l.counts))
	}
}

func TestZeroCapsAndNilLimiterAllowEverything(t *testing.T) {
	l, _ := newLimiter(0, 0)
	if got := allowN(l, victim, 50); got != 50 {
		t.Errorf("caps off allowed %d of 50", got)
	}
	var none *Limiter
	if err := none.Allow(victim); err != nil {
		t.Errorf("nil limiter: %v", err)
	}
}

func TestExceededNeverNamesTheNumber(t *testing.T) {
	l, _ := newLimiter(1, 1)
	l.Allow(victim)
	for _, number := range []string{victim, other} {
		err := l.Allow(number)
		if err == nil {
			t.Fatalf("%s allowed", number)
		}
		if strings.Contains(err.Error(), "4567") || strings.Contains(err.Error(), "2233") {
			t.Errorf("error %q names the number", err)
		}
	}
	if got, want := l.String(), "1 per number and 1 in total per 10m0s"; got != want {
		t.Errorf("String() = %q, want %q", got, want)
	}
}

// Many goroutines at once never get past the caps (run with -race).
func TestConcurrentUseKeepsTheCaps(t *testing.T) {
	l := &Limiter{PerNumber: 6, Total: 40, Window: 10 * time.Minute}
	var mu sync.Mutex
	perNumber := map[string]int{}
	var wg sync.WaitGroup
	for g := range 50 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			number := fmt.Sprintf("+5841400000%02d", g%10)
			for range 20 {
				if l.Allow(number) == nil {
					mu.Lock()
					perNumber[number]++
					mu.Unlock()
				}
			}
		}()
	}
	wg.Wait()
	total := 0
	for number, n := range perNumber {
		total += n
		if n > 6 {
			t.Errorf("%s allowed %d times, want at most 6", number, n)
		}
	}
	if total != 40 {
		t.Errorf("allowed %d in total, want 40 (10 numbers x 6 is more than the total cap)", total)
	}
}
