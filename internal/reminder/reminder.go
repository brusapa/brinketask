// Package reminder computes when a reminder fires (SPEC section 6,
// "Computing the fire instant"). It is pure: the task, the user's settings
// and "now" come in as values, so the table tests fix all of them.
package reminder

import (
	"time"

	"github.com/brusapa/brinketask/internal/localtime"
)

// Kinds of reminder (SPEC section 4).
const (
	KindRelative = "relative"
	KindAbsolute = "absolute"
	KindSnooze   = "snooze"
)

// Reminder is the part of a reminder the computation reads.
type Reminder struct {
	Kind string
	// OffsetMinutes is set for relative reminders: minutes before the due
	// instant (0 to 40320).
	OffsetMinutes int
	// At is set for absolute and snooze reminders.
	At time.Time
}

// Task is the part of the task the computation reads.
type Task struct {
	// Pending is false when the task is not open or is deleted: nothing
	// fires for it.
	Pending bool
	// DueDate is a calendar date at midnight UTC, or nil for no date.
	DueDate *time.Time
	// DueMinutes is the due time as minutes from midnight, or nil for an
	// all-day task.
	DueMinutes *int
	// DueZone is set for a fixed time (due_tz), nil for a floating one.
	DueZone *time.Location
}

// User is the part of the profile the computation reads.
type User struct {
	Zone *time.Location
	// AllDayMinutes is all_day_reminder_time as minutes from midnight.
	AllDayMinutes int
}

// NextFire is the reminder's next_fire_at, or nil when nothing is pending:
// the task is not pending, a relative reminder has no due date to count
// from (D-26), or the instant is already past (SPEC section 6).
//
//   - fixed time: the due instant minus the offset;
//   - floating time: the due date and time in the user's zone, minus the
//     offset;
//   - all-day: the due date at the user's default reminder time in their
//     zone, minus the offset (0 is that day, 1440 the day before);
//   - absolute and snooze: their own instant.
func NextFire(r Reminder, t Task, u User, now time.Time) *time.Time {
	if !t.Pending {
		return nil
	}
	var fire time.Time
	switch r.Kind {
	case KindAbsolute, KindSnooze:
		fire = r.At
	default: // KindRelative
		if t.DueDate == nil {
			return nil
		}
		var due time.Time
		switch {
		case t.DueMinutes == nil:
			due = localtime.Instant(*t.DueDate, u.AllDayMinutes, u.Zone)
		case t.DueZone != nil:
			due = localtime.Instant(*t.DueDate, *t.DueMinutes, t.DueZone)
		default:
			due = localtime.Instant(*t.DueDate, *t.DueMinutes, u.Zone)
		}
		fire = due.Add(-time.Duration(r.OffsetMinutes) * time.Minute)
	}
	if fire.Before(now) {
		return nil
	}
	fire = fire.UTC()
	return &fire
}
