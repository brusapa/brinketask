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
