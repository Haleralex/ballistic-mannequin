// Package ratelimit keeps one token bucket per key (an IP address) and forgets idle ones.
package ratelimit

import (
	"context"
	"math"
	"sync"
	"time"

	"golang.org/x/time/rate"
)

type entry struct {
	lim  *rate.Limiter
	seen time.Time
}

// Limiter allows perMinute requests per key per minute on average, with bursts of up to perMinute.
type Limiter struct {
	mu      sync.Mutex
	buckets map[string]*entry
	r       rate.Limit
	burst   int
	now     func() time.Time
}

// New makes a limiter of perMinute requests per minute per key.
func New(perMinute int) *Limiter {
	if perMinute < 1 {
		perMinute = 1
	}
	return &Limiter{
		buckets: map[string]*entry{},
		r:       rate.Limit(float64(perMinute) / 60),
		burst:   perMinute,
		now:     time.Now,
	}
}

// SetClock replaces the clock (for tests).
func (l *Limiter) SetClock(now func() time.Time) { l.now = now }

// Allow takes a token for key. When none is left it returns false and how long until the next one.
func (l *Limiter) Allow(key string) (bool, time.Duration) {
	now := l.now()
	l.mu.Lock()
	e, ok := l.buckets[key]
	if !ok {
		e = &entry{lim: rate.NewLimiter(l.r, l.burst)}
		l.buckets[key] = e
	}
	e.seen = now
	l.mu.Unlock()
	if e.lim.AllowN(now, 1) {
		return true, 0
	}
	missing := 1 - e.lim.TokensAt(now)
	wait := time.Duration(math.Ceil(missing / float64(l.r) * float64(time.Second)))
	return false, wait
}

// Sweep forgets keys idle for longer than idle (a full bucket refills within a minute anyway).
func (l *Limiter) Sweep(idle time.Duration) {
	cut := l.now().Add(-idle)
	l.mu.Lock()
	defer l.mu.Unlock()
	for k, e := range l.buckets {
		if e.seen.Before(cut) {
			delete(l.buckets, k)
		}
	}
}

// Len is the number of keys tracked.
func (l *Limiter) Len() int {
	l.mu.Lock()
	defer l.mu.Unlock()
	return len(l.buckets)
}

// Janitor sweeps the limiters every interval until ctx is done.
func Janitor(ctx context.Context, interval, idle time.Duration, ls ...*Limiter) {
	t := time.NewTicker(interval)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			for _, l := range ls {
				l.Sweep(idle)
			}
		}
	}
}
