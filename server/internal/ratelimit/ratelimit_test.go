package ratelimit

import (
	"testing"
	"time"
)

type clock struct{ t time.Time }

func (c *clock) now() time.Time      { return c.t }
func (c *clock) add(d time.Duration) { c.t = c.t.Add(d) }
func newWithClock(n int) (*Limiter, *clock) {
	c := &clock{t: time.Date(2026, 10, 18, 12, 0, 0, 0, time.UTC)}
	l := New(n)
	l.SetClock(c.now)
	return l, c
}

func TestBurstThenRefill(t *testing.T) {
	l, c := newWithClock(10) // one token per 6 s
	for i := 0; i < 10; i++ {
		if ok, _ := l.Allow("1.2.3.4"); !ok {
			t.Fatalf("request %d refused", i)
		}
	}
	ok, wait := l.Allow("1.2.3.4")
	if ok || wait <= 0 || wait > 6*time.Second {
		t.Fatalf("11th: ok %v wait %v", ok, wait)
	}
	if ok, _ := l.Allow("5.6.7.8"); !ok {
		t.Fatal("another key must have its own bucket")
	}
	c.add(6 * time.Second)
	if ok, _ := l.Allow("1.2.3.4"); !ok {
		t.Fatal("token not refilled after 6 s")
	}
	if ok, _ := l.Allow("1.2.3.4"); ok {
		t.Fatal("only one token should have refilled")
	}
}

func TestSweep(t *testing.T) {
	l, c := newWithClock(60)
	l.Allow("a")
	c.add(5 * time.Minute)
	l.Allow("b")
	l.Sweep(time.Minute)
	if l.Len() != 1 {
		t.Fatalf("%d keys left", l.Len())
	}
}
