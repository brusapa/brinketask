// Package recurrence is the recurrence engine of SPEC section 5: it parses
// the supported RRULE subset and computes the next due date of a recurring
// task. It is our own (D-11) because the end-of-month rule R-1 departs from
// RFC 5545.
//
// Everything here works on calendar dates, represented as time.Time at
// midnight UTC, the way package tasks stores due dates. Times of day and
// zones do not enter: the due time is kept as it is across occurrences
// (R-4), so the engine never needs it.
package recurrence

import (
	"errors"
	"fmt"
	"slices"
	"strconv"
	"strings"
	"time"
)

// Freq is the FREQ part of a rule.
type Freq int

// The frequencies of the subset.
const (
	Daily Freq = iota + 1
	Weekly
	Monthly
	Yearly
)

var freqNames = map[Freq]string{Daily: "DAILY", Weekly: "WEEKLY", Monthly: "MONTHLY", Yearly: "YEARLY"}

// LastDay is the BYMONTHDAY value -1, "the last day of the month".
const LastDay = -1

// MaxInterval is the largest INTERVAL accepted (SPEC section 5).
const MaxInterval = 999

// Rule is a parsed rule of the subset. Zero values mean "absent":
// ByMonthDay 0 is no BYMONTHDAY, Count 0 is no COUNT, Until nil no UNTIL.
type Rule struct {
	Freq       Freq
	Interval   int            // 1 to MaxInterval
	ByDay      []time.Weekday // WEEKLY only; sorted Monday first, no repeats
	ByMonthDay int            // MONTHLY only; 1 to 31, or LastDay
	Count      int
	Until      *time.Time // a date, inclusive (D-59)
}

// HasByParts reports whether the rule uses BYDAY or BYMONTHDAY, which the
// completion mode does not accept (R-3).
func (r Rule) HasByParts() bool {
	return len(r.ByDay) > 0 || r.ByMonthDay != 0
}

// weekdayNames maps RRULE day names to Go weekdays. time.Weekday counts
// from Sunday = 0.
var weekdayNames = map[string]time.Weekday{
	"MO": time.Monday, "TU": time.Tuesday, "WE": time.Wednesday, "TH": time.Thursday,
	"FR": time.Friday, "SA": time.Saturday, "SU": time.Sunday,
}

// mondayFirst gives a weekday's place in a week that starts on Monday (the
// RFC 5545 default WKST): Monday 0 … Sunday 6.
func mondayFirst(d time.Weekday) int {
	return (int(d) + 6) % 7
}

// Parse reads a rule of the subset. Part names and values are not case
// sensitive (RFC 5545); anything outside the subset is an error whose
// message can be shown to an API client.
func Parse(text string) (Rule, error) {
	if strings.TrimSpace(text) == "" {
		return Rule{}, errors.New("empty rule")
	}
	var r Rule
	seen := map[string]bool{}
	for part := range strings.SplitSeq(strings.ToUpper(text), ";") {
		name, value, found := strings.Cut(part, "=")
		if !found || name == "" || value == "" {
			return Rule{}, fmt.Errorf("%q is not NAME=VALUE", part)
		}
		if seen[name] {
			return Rule{}, fmt.Errorf("%s appears twice", name)
		}
		seen[name] = true

		var err error
		switch name {
		case "FREQ":
			err = r.parseFreq(value)
		case "INTERVAL":
			r.Interval, err = parseInt(value, 1, MaxInterval)
		case "BYDAY":
			err = r.parseByDay(value)
		case "BYMONTHDAY":
			r.ByMonthDay, err = parseInt(value, LastDay, 31)
			if err == nil && r.ByMonthDay == 0 {
				err = errors.New("0 is not a day of the month")
			}
		case "COUNT":
			r.Count, err = parseInt(value, 1, 1_000_000)
		case "UNTIL":
			err = r.parseUntil(value)
		default:
			return Rule{}, fmt.Errorf("%s is not supported", name)
		}
		if err != nil {
			return Rule{}, fmt.Errorf("%s: %w", name, err)
		}
	}

	switch {
	case r.Freq == 0:
		return Rule{}, errors.New("FREQ is required")
	case len(r.ByDay) > 0 && r.Freq != Weekly:
		return Rule{}, errors.New("BYDAY needs FREQ=WEEKLY")
	case r.ByMonthDay != 0 && r.Freq != Monthly:
		return Rule{}, errors.New("BYMONTHDAY needs FREQ=MONTHLY")
	case r.Count != 0 && r.Until != nil:
		return Rule{}, errors.New("COUNT and UNTIL exclude each other")
	}
	if r.Interval == 0 {
		r.Interval = 1
	}
	return r, nil
}

func (r *Rule) parseFreq(value string) error {
	for freq, name := range freqNames {
		if name == value {
			r.Freq = freq
			return nil
		}
	}
	return fmt.Errorf("%s is not DAILY, WEEKLY, MONTHLY or YEARLY", value)
}

func (r *Rule) parseByDay(value string) error {
	for name := range strings.SplitSeq(value, ",") {
		day, ok := weekdayNames[name]
		if !ok {
			// Ordinals such as 1MO or -1FR are outside the subset too.
			return fmt.Errorf("%q is not a day (MO to SU, without a number)", name)
		}
		if slices.Contains(r.ByDay, day) {
			return fmt.Errorf("%s appears twice", name)
		}
		r.ByDay = append(r.ByDay, day)
	}
	slices.SortFunc(r.ByDay, func(a, b time.Weekday) int { return mondayFirst(a) - mondayFirst(b) })
	return nil
}

func (r *Rule) parseUntil(value string) error {
	// D-59: the date form only; time.Parse rejects "20261231T235959Z" here
	// because the layout has no time part.
	date, err := time.Parse("20060102", value)
	if err != nil {
		return errors.New("must be a date, YYYYMMDD")
	}
	r.Until = &date
	return nil
}

// parseInt reads a decimal integer within [lowest, highest], with no sign
// other than a leading minus.
func parseInt(value string, lowest, highest int) (int, error) {
	if strings.HasPrefix(value, "+") {
		return 0, fmt.Errorf("%q is not a number", value)
	}
	n, err := strconv.Atoi(value)
	if err != nil {
		return 0, fmt.Errorf("%q is not a number", value)
	}
	if n < lowest || n > highest {
		return 0, fmt.Errorf("%d is out of range %d to %d", n, lowest, highest)
	}
	return n, nil
}

// String writes the rule in its canonical form (D-59): parts in a fixed
// order, upper case, days Monday first, INTERVAL left out when it is 1.
func (r Rule) String() string {
	parts := []string{"FREQ=" + freqNames[r.Freq]}
	if r.Interval > 1 {
		parts = append(parts, "INTERVAL="+strconv.Itoa(r.Interval))
	}
	if len(r.ByDay) > 0 {
		names := make([]string, len(r.ByDay))
		for i, day := range r.ByDay {
			names[i] = strings.ToUpper(day.String()[:2])
		}
		parts = append(parts, "BYDAY="+strings.Join(names, ","))
	}
	if r.ByMonthDay != 0 {
		parts = append(parts, "BYMONTHDAY="+strconv.Itoa(r.ByMonthDay))
	}
	if r.Count != 0 {
		parts = append(parts, "COUNT="+strconv.Itoa(r.Count))
	}
	if r.Until != nil {
		parts = append(parts, "UNTIL="+r.Until.Format("20060102"))
	}
	return strings.Join(parts, ";")
}
