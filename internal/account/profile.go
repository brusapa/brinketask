package account

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"

	// Time zone names are checked against the IANA database embedded in
	// the binary, so the result does not depend on the host having
	// /usr/share/zoneinfo.
	_ "time/tzdata"

	"github.com/brusapa/brinketask/internal/storage/dbgen"
)

// ErrNotFound means the user does not exist.
var ErrNotFound = errors.New("account: user not found")

// ErrInvalidTimezone means a time zone is not an IANA zone name.
var ErrInvalidTimezone = errors.New("account: not an IANA time zone")

// ErrInvalidTime means a local time is not HH:MM.
var ErrInvalidTime = errors.New("account: not a HH:MM time")

// Profile is what /me shows of a user.
type Profile struct {
	ID          uuid.UUID
	Email       *string
	DisplayName *string
	Timezone    string
	// AllDayReminderTime is "HH:MM".
	AllDayReminderTime string
	InboxListID        uuid.UUID
}

// Settings is a partial update of the profile: nil fields are left alone
// (JSON Merge Patch, D-05).
type Settings struct {
	Timezone           *string
	AllDayReminderTime *string // "HH:MM"
}

// Profile returns the user's profile.
func (s *Service) Profile(ctx context.Context, userID uuid.UUID) (Profile, error) {
	q := dbgen.New(s.pool)
	user, err := q.GetUser(ctx, userID)
	if errors.Is(err, pgx.ErrNoRows) {
		return Profile{}, ErrNotFound
	}
	if err != nil {
		return Profile{}, fmt.Errorf("account: get user: %w", err)
	}
	return s.profile(ctx, q, user)
}

// UpdateSettings applies a partial update and returns the new profile. It
// returns ErrInvalidTimezone or ErrInvalidTime, and changes nothing, when a
// value is invalid.
//
// Changing either setting will recompute pending reminders once they exist
// (phase 5); in phase 1 there are none.
func (s *Service) UpdateSettings(ctx context.Context, userID uuid.UUID, settings Settings) (Profile, error) {
	params := dbgen.UpdateUserSettingsParams{ID: userID, Now: s.clock.Now()}
	if settings.Timezone != nil {
		if err := ValidateTimezone(*settings.Timezone); err != nil {
			return Profile{}, err
		}
		params.Timezone = settings.Timezone
	}
	if settings.AllDayReminderTime != nil {
		t, err := parseLocalTime(*settings.AllDayReminderTime)
		if err != nil {
			return Profile{}, err
		}
		params.AllDayReminderTime = t
	}

	q := dbgen.New(s.pool)
	user, err := q.UpdateUserSettings(ctx, params)
	if errors.Is(err, pgx.ErrNoRows) {
		return Profile{}, ErrNotFound
	}
	if err != nil {
		return Profile{}, fmt.Errorf("account: update settings: %w", err)
	}
	return s.profile(ctx, q, user)
}

func (s *Service) profile(ctx context.Context, q *dbgen.Queries, user dbgen.User) (Profile, error) {
	inboxID, err := q.GetInboxID(ctx, user.ID)
	if err != nil {
		// Sign-up creates the inbox with the user, so this is a real error.
		return Profile{}, fmt.Errorf("account: get inbox: %w", err)
	}
	return Profile{
		ID:                 user.ID,
		Email:              user.Email,
		DisplayName:        user.DisplayName,
		Timezone:           user.Timezone,
		AllDayReminderTime: formatLocalTime(user.AllDayReminderTime),
		InboxListID:        inboxID,
	}, nil
}

// ValidateTimezone accepts IANA zone names such as "Europe/Madrid" or
// "UTC". time.LoadLocation also accepts "" and "Local" (the server's own
// zone), which are not zone names, so they are rejected first.
func ValidateTimezone(name string) error {
	if name == "" || name == "Local" {
		return ErrInvalidTimezone
	}
	if _, err := time.LoadLocation(name); err != nil {
		return ErrInvalidTimezone
	}
	return nil
}

// parseLocalTime converts "HH:MM" to PostgreSQL's time type, which pgx
// represents as microseconds since midnight.
func parseLocalTime(value string) (pgtype.Time, error) {
	// time.Parse also accepts a one-digit hour ("9:00"); the round trip
	// through Format insists on exactly HH:MM.
	t, err := time.Parse("15:04", value)
	if err != nil || t.Format("15:04") != value {
		return pgtype.Time{}, ErrInvalidTime
	}
	micros := (int64(t.Hour())*60 + int64(t.Minute())) * int64(time.Minute/time.Microsecond)
	return pgtype.Time{Microseconds: micros, Valid: true}, nil
}

func formatLocalTime(t pgtype.Time) string {
	minutes := t.Microseconds / int64(time.Minute/time.Microsecond)
	return fmt.Sprintf("%02d:%02d", minutes/60, minutes%60)
}
