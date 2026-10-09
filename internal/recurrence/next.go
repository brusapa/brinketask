package recurrence

import (
	"time"
)

// A series is generated period by period: a period is one day, week, month
// or year according to FREQ, and the series uses every INTERVAL-th period
// counted from the one that holds DTSTART. Each used period holds one
// occurrence, or several for WEEKLY with BYDAY.

// NextDue is the next due date in the "due" mode (R-2): the first
// occurrence of the series that starts on start (DTSTART, D-56) that is
// after current and not before today. Occurrences in between are skipped
// without being counted (D-57). ok is false when the series has no such
// occurrence because of UNTIL; COUNT is the caller's business (R-5), since
// it depends on how many occurrences were completed.
//
// All three dates are calendar dates at midnight UTC.
func NextDue(r Rule, start, current, today time.Time) (next time.Time, ok bool) {
	// "After current and not before today" is "after max(current,
	// yesterday)".
	after := current
	if yesterday := today.AddDate(0, 0, -1); yesterday.After(after) {
		after = yesterday
	}

	// A task years overdue must not walk the series period by period:
	// start a few periods before the answer, found by arithmetic. Periods
	// before the first used one are clamped to it.
	period := periodsBetween(r.Freq, start, after) / r.Interval
	if period > 1 {
		period--
	} else {
		period = 0
	}

	// Each used period has at least one occurrence, and the search starts
	// at most two used periods before the answer, so a few iterations
	// suffice; the bound only guards against a mistake.
	for range 64 {
		for _, occurrence := range occurrences(r, start, period) {
			if r.Until != nil && occurrence.After(*r.Until) {
				return time.Time{}, false
			}
			if occurrence.After(after) {
				return occurrence, true
			}
		}
		period++
	}
	return time.Time{}, false
}

// NextFromCompletion is the next due date in the "completion" mode (R-3):
// the date the task was completed (in the user's zone) plus INTERVAL
// periods, with R-1 for months that lack the day. ok is false when that is
// past UNTIL.
func NextFromCompletion(r Rule, completed time.Time) (next time.Time, ok bool) {
	next = addPeriods(r.Freq, completed, r.Interval, completed.Day())
	if r.Until != nil && next.After(*r.Until) {
		return time.Time{}, false
	}
	return next, true
}

// occurrences lists, in order, the occurrences of the series in the n-th
// used period (n = 0 holds DTSTART). DTSTART itself is always the first
// occurrence, as in RFC 5545, even when it does not match BYDAY or
// BYMONTHDAY; nothing before it belongs to the series.
func occurrences(r Rule, start time.Time, n int) []time.Time {
	periods := n * r.Interval
	switch {
	case r.Freq == Weekly && len(r.ByDay) > 0:
		// The week (Monday first) of DTSTART, moved by whole weeks.
		monday := start.AddDate(0, 0, -mondayFirst(start.Weekday())+7*periods)
		var result []time.Time
		if n == 0 {
			result = append(result, start)
		}
		for _, day := range r.ByDay {
			date := monday.AddDate(0, 0, mondayFirst(day))
			if date.After(start) {
				result = append(result, date)
			}
		}
		return result

	case r.Freq == Monthly && r.ByMonthDay != 0:
		date := withMonthDay(addMonths(start, periods), r.ByMonthDay)
		if n == 0 {
			// DTSTART, then the rule's day of the same month if later.
			if date.After(start) {
				return []time.Time{start, date}
			}
			return []time.Time{start}
		}
		return []time.Time{date}

	default:
		// One occurrence per period, on DTSTART's day (R-1 keeps the 31st
		// or the 29th of February as the desired day).
		return []time.Time{addPeriods(r.Freq, start, periods, start.Day())}
	}
}

// addPeriods moves a date by n periods. For months and years the day is
// the desired one, or the last day of the month when that month is
// shorter (R-1).
func addPeriods(freq Freq, date time.Time, n, desiredDay int) time.Time {
	switch freq {
	case Daily:
		return date.AddDate(0, 0, n)
	case Weekly:
		return date.AddDate(0, 0, 7*n)
	case Monthly:
		return withMonthDay(addMonths(date, n), desiredDay)
	default: // Yearly
		return withMonthDay(addMonths(date, 12*n), desiredDay)
	}
}

// addMonths returns the first day of the month n months after date's.
// Go's AddDate would normalize "31 April" to "1 May", which R-1 forbids,
// so months are moved from day 1 and the day is set afterwards.
func addMonths(date time.Time, n int) time.Time {
	return time.Date(date.Year(), date.Month()+time.Month(n), 1, 0, 0, 0, 0, time.UTC)
}

// withMonthDay sets the day of a month: day, the last day if the month is
// shorter (R-1), or the last day for LastDay.
func withMonthDay(firstOfMonth time.Time, day int) time.Time {
	last := daysIn(firstOfMonth)
	if day == LastDay || day > last {
		day = last
	}
	return firstOfMonth.AddDate(0, 0, day-1)
}

// daysIn is the number of days of a date's month: day 0 of the next month
// is the last day of this one.
func daysIn(date time.Time) int {
	return time.Date(date.Year(), date.Month()+1, 0, 0, 0, 0, 0, time.UTC).Day()
}

// periodsBetween counts whole periods from a to b (b after a), rounding
// down; 0 when b is not after a.
func periodsBetween(freq Freq, a, b time.Time) int {
	if !b.After(a) {
		return 0
	}
	switch freq {
	case Daily:
		return daysBetween(a, b)
	case Weekly:
		return daysBetween(a, b) / 7
	case Monthly:
		return (b.Year()-a.Year())*12 + int(b.Month()) - int(a.Month())
	default: // Yearly
		return b.Year() - a.Year()
	}
}

// daysBetween counts days from a to b. It goes through Unix seconds
// because time.Duration, an int64 of nanoseconds, overflows after about
// 292 years, and a due date can be far older than that.
func daysBetween(a, b time.Time) int {
	return int((b.Unix() - a.Unix()) / (24 * 60 * 60))
}
