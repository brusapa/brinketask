package clock

import (
	"testing"
	"time"
)

// Both implementations must satisfy the interface; this fails to compile
// otherwise.
var (
	_ Clock = System{}
	_ Clock = (*Fixed)(nil)
)

func TestFixedOnlyMovesWhenTold(t *testing.T) {
	start := time.Date(2026, time.March, 29, 0, 59, 0, 0, time.UTC)
	c := NewFixed(start)

	if got := c.Now(); !got.Equal(start) {
		t.Fatalf("Now() = %v, want %v", got, start)
	}
	if got := c.Now(); !got.Equal(start) {
		t.Fatalf("second Now() = %v, want %v: the clock moved by itself", got, start)
	}

	c.Advance(90 * time.Minute)
	if want := start.Add(90 * time.Minute); !c.Now().Equal(want) {
		t.Fatalf("after Advance: Now() = %v, want %v", c.Now(), want)
	}

	later := time.Date(2026, time.October, 25, 1, 0, 0, 0, time.UTC)
	c.Set(later)
	if !c.Now().Equal(later) {
		t.Fatalf("after Set: Now() = %v, want %v", c.Now(), later)
	}
}

func TestSystemIsCurrent(t *testing.T) {
	before := time.Now()
	got := System{}.Now()
	after := time.Now()
	if got.Before(before) || got.After(after) {
		t.Fatalf("System.Now() = %v, outside [%v, %v]", got, before, after)
	}
}
