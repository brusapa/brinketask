package localtime

import (
	"errors"
	"testing"
)

func TestParseAndFormat(t *testing.T) {
	for _, value := range []string{"00:00", "09:05", "23:59"} {
		parsed, err := Parse(value)
		if err != nil {
			t.Fatalf("Parse(%q): %v", value, err)
		}
		if got := Format(parsed); got != value {
			t.Errorf("Format(Parse(%q)) = %q", value, got)
		}
	}
	for _, value := range []string{"24:00", "9:00", "09:60", "noon", "", "09:00:00"} {
		if _, err := Parse(value); !errors.Is(err, ErrInvalidTime) {
			t.Errorf("Parse(%q) = %v, want ErrInvalidTime", value, err)
		}
	}
}

func TestValidateZone(t *testing.T) {
	for _, zone := range []string{"UTC", "Europe/Madrid", "America/Argentina/Buenos_Aires"} {
		if err := ValidateZone(zone); err != nil {
			t.Errorf("ValidateZone(%q) = %v", zone, err)
		}
	}
	for _, zone := range []string{"", "Local", "Mars/Olympus_Mons", "Europe/../etc/passwd", "europe/madrid"} {
		if err := ValidateZone(zone); !errors.Is(err, ErrInvalidZone) {
			t.Errorf("ValidateZone(%q) = %v, want ErrInvalidZone", zone, err)
		}
	}
}
