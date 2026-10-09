package recurrence

import (
	"strings"
	"testing"
	"time"
)

// d parses "YYYY-MM-DD" as a date at midnight UTC.
func d(t *testing.T, s string) time.Time {
	t.Helper()
	date, err := time.Parse(time.DateOnly, s)
	if err != nil {
		t.Fatal(err)
	}
	return date
}

func mustParse(t *testing.T, s string) Rule {
	t.Helper()
	r, err := Parse(s)
	if err != nil {
		t.Fatalf("Parse(%q): %v", s, err)
	}
	return r
}

func TestParseAndCanonicalForm(t *testing.T) {
	tests := []struct{ in, canonical string }{
		{"FREQ=DAILY", "FREQ=DAILY"},
		{"freq=weekly;byday=we,mo", "FREQ=WEEKLY;BYDAY=MO,WE"},
		{"BYDAY=SU,SA;FREQ=WEEKLY;INTERVAL=2", "FREQ=WEEKLY;INTERVAL=2;BYDAY=SA,SU"},
		{"FREQ=MONTHLY;INTERVAL=1;BYMONTHDAY=-1", "FREQ=MONTHLY;BYMONTHDAY=-1"},
		{"FREQ=MONTHLY;BYMONTHDAY=31;COUNT=12", "FREQ=MONTHLY;BYMONTHDAY=31;COUNT=12"},
		{"FREQ=YEARLY;UNTIL=20301231", "FREQ=YEARLY;UNTIL=20301231"},
		{"FREQ=DAILY;INTERVAL=999", "FREQ=DAILY;INTERVAL=999"},
	}
	for _, tt := range tests {
		r := mustParse(t, tt.in)
		if got := r.String(); got != tt.canonical {
			t.Errorf("Parse(%q).String() = %q, want %q", tt.in, got, tt.canonical)
		}
		// The canonical form reads back to itself.
		if again := mustParse(t, r.String()).String(); again != tt.canonical {
			t.Errorf("round trip of %q gave %q", tt.canonical, again)
		}
	}
}

// SPEC section 5: anything outside the subset is rejected (422 upstream).
func TestParseRejectsOutsideTheSubset(t *testing.T) {
	for _, in := range []string{
		"",
		"FREQ=HOURLY",
		"FREQ=SECONDLY",
		"INTERVAL=2",                        // no FREQ
		"FREQ=DAILY;INTERVAL=0",             // range
		"FREQ=DAILY;INTERVAL=1000",          // range
		"FREQ=DAILY;INTERVAL=+2",            // sign
		"FREQ=DAILY;INTERVAL=x",             // not a number
		"FREQ=DAILY;BYDAY=MO",               // BYDAY needs WEEKLY
		"FREQ=WEEKLY;BYDAY=1MO",             // ordinal
		"FREQ=WEEKLY;BYDAY=MO,MO",           // repeated day
		"FREQ=WEEKLY;BYDAY=XX",              // unknown day
		"FREQ=WEEKLY;BYMONTHDAY=3",          // BYMONTHDAY needs MONTHLY
		"FREQ=MONTHLY;BYMONTHDAY=0",         // no day 0
		"FREQ=MONTHLY;BYMONTHDAY=32",        // range
		"FREQ=MONTHLY;BYMONTHDAY=-2",        // only -1 is negative
		"FREQ=MONTHLY;BYMONTHDAY=1,15",      // a single value
		"FREQ=DAILY;COUNT=3;UNTIL=20301231", // exclusive
		"FREQ=DAILY;COUNT=0",                // range
		"FREQ=DAILY;UNTIL=20301231T235959Z", // D-59: date only
		"FREQ=DAILY;UNTIL=20300231",         // no such date
		"FREQ=DAILY;FREQ=WEEKLY",            // repeated part
		"FREQ=DAILY;BYHOUR=9",               // unsupported part
		"FREQ=DAILY;WKST=SU",                // unsupported part
		"FREQ=DAILY;",                       // empty part
		"RRULE:FREQ=DAILY",                  // a property, not a rule value
	} {
		if r, err := Parse(in); err == nil {
			t.Errorf("Parse(%q) accepted it as %q", in, r)
		}
	}
}

func TestHasByParts(t *testing.T) {
	if mustParse(t, "FREQ=WEEKLY").HasByParts() || !mustParse(t, "FREQ=WEEKLY;BYDAY=MO").HasByParts() ||
		!mustParse(t, "FREQ=MONTHLY;BYMONTHDAY=-1").HasByParts() {
		t.Error("HasByParts is wrong")
	}
}

// R-2, with the other rules of section 5 where they apply. "today" is the
// user's date; the task is due on "current"; the series started on "start".
func TestNextDue(t *testing.T) {
	tests := []struct {
		name                  string
		rule                  string
		start, current, today string
		want                  string // "" means the series has ended
	}{
		// Plain frequencies, completed on time.
		{"daily", "FREQ=DAILY", "2026-10-09", "2026-10-09", "2026-10-09", "2026-10-10"},
		{"every 3 days", "FREQ=DAILY;INTERVAL=3", "2026-10-09", "2026-10-09", "2026-10-09", "2026-10-12"},
		{"weekly", "FREQ=WEEKLY", "2026-10-09", "2026-10-09", "2026-10-09", "2026-10-16"},
		{"monthly", "FREQ=MONTHLY", "2026-10-09", "2026-10-09", "2026-10-09", "2026-11-09"},
		{"yearly", "FREQ=YEARLY", "2026-10-09", "2026-10-09", "2026-10-09", "2027-10-09"},
		// Completing early: the next occurrence after the current one.
		{"completed a week early", "FREQ=WEEKLY", "2026-10-16", "2026-10-16", "2026-10-09", "2026-10-23"},

		// WEEKLY with BYDAY, weeks starting on Monday.
		{"Mon,Wed from Mon", "FREQ=WEEKLY;BYDAY=MO,WE", "2026-10-05", "2026-10-05", "2026-10-05", "2026-10-07"},
		{"Mon,Wed from Wed", "FREQ=WEEKLY;BYDAY=MO,WE", "2026-10-05", "2026-10-07", "2026-10-07", "2026-10-12"},
		{"every 2 weeks Mon,Wed", "FREQ=WEEKLY;INTERVAL=2;BYDAY=MO,WE", "2026-10-05", "2026-10-07", "2026-10-07", "2026-10-19"},
		{"DTSTART not on a BYDAY", "FREQ=WEEKLY;BYDAY=MO", "2026-10-08", "2026-10-08", "2026-10-08", "2026-10-12"},
		{"Sunday ends the week", "FREQ=WEEKLY;INTERVAL=2;BYDAY=SU,MO", "2026-10-05", "2026-10-11", "2026-10-11", "2026-10-19"},

		// R-1: the end of the month, and the desired day comes back.
		{"31st to April 30", "FREQ=MONTHLY", "2026-03-31", "2026-03-31", "2026-03-31", "2026-04-30"},
		{"back to May 31", "FREQ=MONTHLY", "2026-03-31", "2026-04-30", "2026-04-30", "2026-05-31"},
		{"31st to February 28", "FREQ=MONTHLY", "2026-01-31", "2026-01-31", "2026-01-31", "2026-02-28"},
		{"back to March 31", "FREQ=MONTHLY", "2026-01-31", "2026-02-28", "2026-02-28", "2026-03-31"},
		{"30th in February", "FREQ=MONTHLY", "2026-01-30", "2026-01-30", "2026-01-30", "2026-02-28"},
		{"BYMONTHDAY=31 in June", "FREQ=MONTHLY;BYMONTHDAY=31", "2026-05-31", "2026-05-31", "2026-05-31", "2026-06-30"},
		{"BYMONTHDAY=-1", "FREQ=MONTHLY;BYMONTHDAY=-1", "2026-01-31", "2026-01-31", "2026-01-31", "2026-02-28"},
		{"BYMONTHDAY later in DTSTART's month", "FREQ=MONTHLY;BYMONTHDAY=20", "2026-10-05", "2026-10-05", "2026-10-05", "2026-10-20"},
		{"BYMONTHDAY earlier than DTSTART", "FREQ=MONTHLY;BYMONTHDAY=3", "2026-10-05", "2026-10-05", "2026-10-05", "2026-11-03"},

		// Leap years.
		{"Feb 29 to Feb 28", "FREQ=YEARLY", "2028-02-29", "2028-02-29", "2028-02-29", "2029-02-28"},
		{"Feb 29 comes back in a leap year", "FREQ=YEARLY", "2028-02-29", "2031-02-28", "2031-02-28", "2032-02-29"},
		{"daily over Feb 29", "FREQ=DAILY", "2028-02-28", "2028-02-28", "2028-02-28", "2028-02-29"},
		{"BYMONTHDAY=-1 in a leap February", "FREQ=MONTHLY;BYMONTHDAY=-1", "2028-01-31", "2028-01-31", "2028-01-31", "2028-02-29"},
		{"BYMONTHDAY=30 in a leap February", "FREQ=MONTHLY;BYMONTHDAY=30", "2028-01-30", "2028-01-30", "2028-01-30", "2028-02-29"},

		// Daylight saving changes do not move dates (R-4 keeps the time
		// of day; the dates are plain calendar dates).
		{"daily over the March change", "FREQ=DAILY", "2026-03-28", "2026-03-28", "2026-03-28", "2026-03-29"},
		{"daily over the October change", "FREQ=DAILY", "2026-10-24", "2026-10-24", "2026-10-24", "2026-10-25"},
		{"weekly over the October change", "FREQ=WEEKLY", "2026-10-21", "2026-10-21", "2026-10-21", "2026-10-28"},

		// R-2: heavily overdue; intermediate occurrences are dropped.
		{"daily, a month late", "FREQ=DAILY", "2026-09-01", "2026-09-01", "2026-10-09", "2026-10-09"},
		{"every 2 days, late, on cycle", "FREQ=DAILY;INTERVAL=2", "2026-09-01", "2026-09-01", "2026-10-09", "2026-10-09"},
		{"every 3 days, late, off cycle", "FREQ=DAILY;INTERVAL=3", "2026-09-01", "2026-09-01", "2026-10-09", "2026-10-10"},
		{"every 4 days, late, off cycle", "FREQ=DAILY;INTERVAL=4", "2026-09-01", "2026-09-01", "2026-10-09", "2026-10-11"},
		{"weekly (Thursdays), years late", "FREQ=WEEKLY", "2016-10-06", "2016-10-06", "2026-10-09", "2026-10-15"},
		{"Mon,Wed, years late", "FREQ=WEEKLY;BYDAY=MO,WE", "2010-01-04", "2010-01-04", "2026-10-09", "2026-10-12"},
		{"monthly 31st, years late", "FREQ=MONTHLY", "2020-01-31", "2020-01-31", "2026-11-05", "2026-11-30"},
		{"yearly, decades late", "FREQ=YEARLY", "1990-10-09", "1990-10-09", "2026-10-10", "2027-10-09"},
		{"due today stays today", "FREQ=DAILY", "2026-10-01", "2026-10-08", "2026-10-09", "2026-10-09"},

		// UNTIL is inclusive; past it the series ends (R-5).
		{"until, last occurrence", "FREQ=DAILY;UNTIL=20261010", "2026-10-09", "2026-10-09", "2026-10-09", "2026-10-10"},
		{"until, ended", "FREQ=DAILY;UNTIL=20261010", "2026-10-09", "2026-10-10", "2026-10-10", ""},
		{"until, overdue past the end", "FREQ=WEEKLY;UNTIL=20261001", "2026-09-01", "2026-09-01", "2026-10-09", ""},
		// COUNT is not checked here: the caller compares it with the
		// completed occurrences (D-57).
		{"count is the caller's", "FREQ=DAILY;COUNT=1", "2026-10-09", "2026-10-09", "2026-10-09", "2026-10-10"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, ok := NextDue(mustParse(t, tt.rule), d(t, tt.start), d(t, tt.current), d(t, tt.today))
			switch {
			case tt.want == "" && ok:
				t.Errorf("got %s, want the end of the series", got.Format(time.DateOnly))
			case tt.want != "" && !ok:
				t.Errorf("series ended, want %s", tt.want)
			case tt.want != "" && got.Format(time.DateOnly) != tt.want:
				t.Errorf("got %s, want %s", got.Format(time.DateOnly), tt.want)
			}
		})
	}
}

// R-3: completion date plus the interval.
func TestNextFromCompletion(t *testing.T) {
	tests := []struct{ rule, completed, want string }{
		{"FREQ=DAILY", "2026-10-09", "2026-10-10"},
		{"FREQ=DAILY;INTERVAL=3", "2026-10-09", "2026-10-12"},
		{"FREQ=WEEKLY;INTERVAL=2", "2026-10-09", "2026-10-23"},
		{"FREQ=MONTHLY", "2026-01-31", "2026-02-28"}, // R-1
		{"FREQ=MONTHLY", "2026-02-28", "2026-03-28"},
		{"FREQ=YEARLY", "2028-02-29", "2029-02-28"},
		{"FREQ=DAILY;UNTIL=20261010", "2026-10-09", "2026-10-10"},
		{"FREQ=DAILY;UNTIL=20261010", "2026-10-10", ""},
		// D-61: a backdated completion can give a past date.
		{"FREQ=DAILY", "2026-09-01", "2026-09-02"},
	}
	for _, tt := range tests {
		got, ok := NextFromCompletion(mustParse(t, tt.rule), d(t, tt.completed))
		if tt.want == "" {
			if ok {
				t.Errorf("%s from %s: got %s, want the end", tt.rule, tt.completed, got.Format(time.DateOnly))
			}
			continue
		}
		if !ok || got.Format(time.DateOnly) != tt.want {
			t.Errorf("%s from %s: got %s (%v), want %s", tt.rule, tt.completed, got.Format(time.DateOnly), ok, tt.want)
		}
	}
}

// Walking a whole series occurrence by occurrence gives the dates RFC 5545
// would, except where R-1 departs from it.
func TestWalkSeries(t *testing.T) {
	r := mustParse(t, "FREQ=MONTHLY")
	start := d(t, "2026-01-31")
	current := start
	var dates []string
	for range 12 {
		current, _ = NextDue(r, start, current, current)
		dates = append(dates, current.Format("01-02"))
	}
	want := "02-28 03-31 04-30 05-31 06-30 07-31 08-31 09-30 10-31 11-30 12-31 01-31"
	if got := strings.Join(dates, " "); got != want {
		t.Errorf("series = %s\n want %s", got, want)
	}
}

// A task due in the distant past finds its next date quickly.
func TestNextDueFarInThePast(t *testing.T) {
	start := d(t, "0001-01-01")
	got, ok := NextDue(mustParse(t, "FREQ=DAILY;INTERVAL=7"), start, start, d(t, "2026-10-09"))
	if !ok || got.Before(d(t, "2026-10-09")) || got.After(d(t, "2026-10-15")) {
		t.Errorf("got %s", got.Format(time.DateOnly))
	}
}
