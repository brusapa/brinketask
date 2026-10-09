// Package localtime converts the API's local times of day ("HH:MM", SPEC
// section 8) to and from PostgreSQL's time type, and checks time zone
// names.
package localtime

import (
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5/pgtype"

	// Time zone names are checked against the IANA database embedded in
	// the binary, so the result does not depend on the host having
	// /usr/share/zoneinfo.
	_ "time/tzdata"
)

// ErrInvalidTime means a local time is not HH:MM.
var ErrInvalidTime = errors.New("localtime: not a HH:MM time")

// ErrInvalidZone means a time zone is not an IANA zone name.
var ErrInvalidZone = errors.New("localtime: not an IANA time zone")

// microsPerMinute converts between minutes and pgtype.Time's unit.
const microsPerMinute = int64(time.Minute / time.Microsecond)

// Parse converts "HH:MM" to PostgreSQL's time, which pgx represents as
// microseconds since midnight.
func Parse(value string) (pgtype.Time, error) {
	// time.Parse also accepts a one-digit hour ("9:00"); the round trip
	// through Format insists on exactly HH:MM.
	t, err := time.Parse("15:04", value)
	if err != nil || t.Format("15:04") != value {
		return pgtype.Time{}, ErrInvalidTime
	}
	minutes := int64(t.Hour())*60 + int64(t.Minute())
	return pgtype.Time{Microseconds: minutes * microsPerMinute, Valid: true}, nil
}

// Format converts PostgreSQL's time to "HH:MM". Seconds, which the API
// never stores, are dropped.
func Format(t pgtype.Time) string {
	minutes := t.Microseconds / microsPerMinute
	return fmt.Sprintf("%02d:%02d", minutes/60, minutes%60)
}

// ValidateZone accepts IANA zone names such as "Europe/Madrid" or "UTC".
// time.LoadLocation also accepts "" and "Local" (the server's own zone),
// which are not zone names, so they are rejected first.
func ValidateZone(name string) error {
	if name == "" || name == "Local" {
		return ErrInvalidZone
	}
	if _, err := time.LoadLocation(name); err != nil {
		return ErrInvalidZone
	}
	return nil
}

// Instant is the moment a wall-clock time happens on a calendar date in a
// zone. Daylight saving changes make some wall-clock times ambiguous, and
// it resolves them as RFC 5545 and the web client do ("compatible"):
//   - a time skipped by the spring change (02:30 on the last Sunday of
//     March in Madrid) moves forward by the gap: 03:30;
//   - a time that happens twice in autumn takes the first one.
//
// date is a calendar date at midnight UTC; minutes counts from midnight.
// Go's time.Date leaves both cases unspecified, so the offsets on either
// side are tried explicitly.
func Instant(date time.Time, minutes int, zone *time.Location) time.Time {
	// The wall-clock time read as if it were UTC.
	wall := time.Date(date.Year(), date.Month(), date.Day(), 0, minutes, 0, 0, time.UTC)
	// A day before and after are on either side of any change.
	_, before := wall.Add(-24 * time.Hour).In(zone).Zone()
	_, after := wall.Add(24 * time.Hour).In(zone).Zone()

	early := wall.Add(-time.Duration(before) * time.Second)
	late := wall.Add(-time.Duration(after) * time.Second)
	earlyOK := sameWallClock(early, wall, zone)
	lateOK := sameWallClock(late, wall, zone)
	switch {
	case earlyOK && lateOK:
		// An autumn overlap, or no change at all (both equal): the first.
		if late.Before(early) {
			return late
		}
		return early
	case earlyOK:
		return early
	case lateOK:
		return late
	default:
		// A spring gap: read with the offset in force before the change,
		// which lands as far after the gap as the time was into it.
		return early
	}
}

func sameWallClock(instant, wall time.Time, zone *time.Location) bool {
	local := instant.In(zone)
	return local.Year() == wall.Year() && local.Month() == wall.Month() && local.Day() == wall.Day() &&
		local.Hour() == wall.Hour() && local.Minute() == wall.Minute()
}

// Minutes returns a PostgreSQL time of day as minutes from midnight.
func Minutes(t pgtype.Time) int {
	return int(t.Microseconds / int64(time.Minute/time.Microsecond))
}
