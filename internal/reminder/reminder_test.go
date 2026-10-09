package reminder

import (
	"testing"
	"time"

	"github.com/brusapa/brinketask/internal/localtime"
)

func zone(t *testing.T, name string) *time.Location {
	t.Helper()
	loc, err := time.LoadLocation(name)
	if err != nil {
		t.Fatal(err)
	}
	return loc
}

func date(t *testing.T, s string) *time.Time {
	t.Helper()
	d, err := time.Parse(time.DateOnly, s)
	if err != nil {
		t.Fatal(err)
	}
	return &d
}

func minutes(h, m int) *int {
	v := h*60 + m
	return &v
}

// SPEC section 11: next_fire_at for the four due types, in Madrid, across
// both daylight saving changes.
func TestNextFire(t *testing.T) {
	madrid := zone(t, "Europe/Madrid")
	tokyo := zone(t, "Asia/Tokyo")
	user := User{Zone: madrid, AllDayMinutes: 9 * 60}
	now := time.Date(2026, 3, 1, 0, 0, 0, 0, time.UTC)
	relative := func(offset int) Reminder { return Reminder{Kind: KindRelative, OffsetMinutes: offset} }

	tests := []struct {
		name string
		r    Reminder
		task Task
		want string // RFC 3339 in UTC, or "" for none
	}{
		{"fixed time", relative(15), Task{Pending: true, DueDate: date(t, "2026-03-10"), DueMinutes: minutes(10, 0), DueZone: tokyo},
			"2026-03-10T00:45:00Z"},
		{"floating time in the user's zone", relative(15), Task{Pending: true, DueDate: date(t, "2026-03-10"), DueMinutes: minutes(10, 0)},
			"2026-03-10T08:45:00Z"},
		{"all-day, that day", relative(0), Task{Pending: true, DueDate: date(t, "2026-03-10")},
			"2026-03-10T08:00:00Z"},
		{"all-day, the day before", relative(1440), Task{Pending: true, DueDate: date(t, "2026-03-10")},
			"2026-03-09T08:00:00Z"},
		{"no date", relative(0), Task{Pending: true}, ""},
		{"absolute", Reminder{Kind: KindAbsolute, At: time.Date(2026, 3, 5, 7, 0, 0, 0, time.UTC)}, Task{Pending: true},
			"2026-03-05T07:00:00Z"},
		{"snooze", Reminder{Kind: KindSnooze, At: time.Date(2026, 3, 1, 0, 10, 0, 0, time.UTC)}, Task{Pending: true},
			"2026-03-01T00:10:00Z"},
		{"task not open", relative(0), Task{Pending: false, DueDate: date(t, "2026-03-10")}, ""},
		{"already past", relative(0), Task{Pending: true, DueDate: date(t, "2026-02-27")}, ""},
		{"absolute in the past (D-67)", Reminder{Kind: KindAbsolute, At: now.Add(-time.Minute)}, Task{Pending: true}, ""},
		{"exactly now still fires", Reminder{Kind: KindAbsolute, At: now}, Task{Pending: true}, "2026-03-01T00:00:00Z"},

		// Spring forward: Sunday 2026-03-29, 02:00 -> 03:00 in Madrid.
		{"all-day across the spring change", relative(0), Task{Pending: true, DueDate: date(t, "2026-03-29")},
			"2026-03-29T07:00:00Z"}, // 09:00 CEST
		{"the day before, still winter", relative(1440), Task{Pending: true, DueDate: date(t, "2026-03-29")},
			"2026-03-28T07:00:00Z"}, // 24 h before 09:00 CEST is 08:00 CET
		{"floating time in the gap", relative(0), Task{Pending: true, DueDate: date(t, "2026-03-29"), DueMinutes: minutes(2, 30)},
			"2026-03-29T01:30:00Z"}, // 03:30 CEST
		// Fall back: Sunday 2026-10-25, 03:00 -> 02:00 in Madrid.
		{"floating time in the overlap takes the first", relative(0), Task{Pending: true, DueDate: date(t, "2026-10-25"), DueMinutes: minutes(2, 30)},
			"2026-10-25T00:30:00Z"}, // 02:30 CEST
		{"all-day after the autumn change", relative(0), Task{Pending: true, DueDate: date(t, "2026-10-26")},
			"2026-10-26T08:00:00Z"}, // 09:00 CET
		{"fixed time keeps its zone across the change", relative(60), Task{Pending: true, DueDate: date(t, "2026-10-26"), DueMinutes: minutes(9, 0), DueZone: madrid},
			"2026-10-26T07:00:00Z"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := NextFire(tt.r, tt.task, user, now)
			switch {
			case tt.want == "" && got != nil:
				t.Errorf("got %s, want none", got.Format(time.RFC3339))
			case tt.want != "" && (got == nil || got.Format(time.RFC3339) != tt.want):
				t.Errorf("got %v, want %s", got, tt.want)
			}
		})
	}
}

// SPEC section 11: a change of the user's zone moves floating and all-day
// reminders, not fixed ones.
func TestUserZoneChange(t *testing.T) {
	now := time.Date(2026, 3, 1, 0, 0, 0, 0, time.UTC)
	floating := Task{Pending: true, DueDate: date(t, "2026-03-10"), DueMinutes: minutes(9, 0)}
	allDay := Task{Pending: true, DueDate: date(t, "2026-03-10")}
	fixed := Task{Pending: true, DueDate: date(t, "2026-03-10"), DueMinutes: minutes(9, 0), DueZone: zone(t, "Europe/Madrid")}
	r := Reminder{Kind: KindRelative}
	madrid := User{Zone: zone(t, "Europe/Madrid"), AllDayMinutes: 9 * 60}
	newYork := User{Zone: zone(t, "America/New_York"), AllDayMinutes: 9 * 60}
	for name, task := range map[string]Task{"floating": floating, "all-day": allDay} {
		a, b := NextFire(r, task, madrid, now), NextFire(r, task, newYork, now)
		// On 10 March New York is already on summer time (UTC-4) and Madrid
		// not yet (UTC+1): 5 hours apart.
		if a == nil || b == nil || b.Sub(*a) != 5*time.Hour {
			t.Errorf("%s: Madrid %v, New York %v; want 5 h apart", name, a, b)
		}
	}
	if a, b := NextFire(r, fixed, madrid, now), NextFire(r, fixed, newYork, now); a == nil || b == nil || !a.Equal(*b) {
		t.Errorf("fixed: %v and %v differ", a, b)
	}
	// The default time moves all-day reminders.
	early := madrid
	early.AllDayMinutes = 7 * 60
	if a, b := NextFire(r, allDay, madrid, now), NextFire(r, allDay, early, now); a == nil || b == nil || a.Sub(*b) != 2*time.Hour {
		t.Errorf("default time: %v, %v", a, b)
	}
}

func TestInstantIsUsedForLocalTimes(t *testing.T) {
	// A sanity check that localtime.Instant reads "minutes from midnight".
	got := localtime.Instant(*date(t, "2026-07-01"), 90, time.UTC)
	if got.Format(time.RFC3339) != "2026-07-01T01:30:00Z" {
		t.Errorf("Instant = %s", got.Format(time.RFC3339))
	}
}
