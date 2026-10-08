// Package clock is the injected clock of D-17: domain code (sessions,
// recurrence, reminders) asks a Clock for the time instead of calling
// time.Now, so tests can pin it.
package clock

import (
	"sync"
	"time"
)

// Clock returns the current time.
type Clock interface {
	Now() time.Time
}

// System is the real clock, used in production.
type System struct{}

// Now returns the operating system's time.
func (System) Now() time.Time {
	return time.Now()
}

// Fixed is a clock for tests: it returns the time it was set to and only
// moves when the test moves it. It is safe for concurrent use, because the
// code under test may read it from several goroutines.
type Fixed struct {
	mu  sync.Mutex
	now time.Time
}

// NewFixed returns a Fixed clock set to now. It is a pointer so that every
// holder sees the changes made by Set and Advance.
func NewFixed(now time.Time) *Fixed {
	return &Fixed{now: now}
}

// Now returns the time the clock is set to.
func (c *Fixed) Now() time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.now
}

// Set moves the clock to now.
func (c *Fixed) Set(now time.Time) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.now = now
}

// Advance moves the clock forward by d (backward if d is negative).
func (c *Fixed) Advance(d time.Duration) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.now = c.now.Add(d)
}
