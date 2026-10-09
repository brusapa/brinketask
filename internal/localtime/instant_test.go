package localtime

import (
	"testing"
	"time"
)

func TestInstant(t *testing.T) {
	madrid, err := time.LoadLocation("Europe/Madrid")
	if err != nil {
		t.Fatal(err)
	}
	day := func(s string) time.Time {
		d, err := time.Parse(time.DateOnly, s)
		if err != nil {
			t.Fatal(err)
		}
		return d
	}
	tests := []struct {
		name    string
		date    string
		minutes int
		want    string
	}{
		{"winter", "2026-01-15", 9 * 60, "2026-01-15T08:00:00Z"},
		{"summer", "2026-07-15", 9 * 60, "2026-07-15T07:00:00Z"},
		{"before the spring gap", "2026-03-29", 1*60 + 59, "2026-03-29T00:59:00Z"},
		{"in the spring gap: moved forward", "2026-03-29", 2*60 + 30, "2026-03-29T01:30:00Z"},
		{"after the spring gap", "2026-03-29", 3 * 60, "2026-03-29T01:00:00Z"},
		{"in the autumn overlap: the first", "2026-10-25", 2*60 + 30, "2026-10-25T00:30:00Z"},
		{"after the overlap", "2026-10-25", 3 * 60, "2026-10-25T02:00:00Z"},
		{"midnight", "2026-10-25", 0, "2026-10-24T22:00:00Z"},
	}
	for _, tt := range tests {
		if got := Instant(day(tt.date), tt.minutes, madrid).UTC().Format(time.RFC3339); got != tt.want {
			t.Errorf("%s: got %s, want %s", tt.name, got, tt.want)
		}
	}
}
