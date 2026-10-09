package account

import (
	"context"
	"errors"
	"fmt"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/brusapa/brinketask/internal/localtime"
	"github.com/brusapa/brinketask/internal/storage/dbgen"
)

// ErrNotFound means the user does not exist.
var ErrNotFound = errors.New("account: user not found")

// ErrInvalidTimezone means a time zone is not an IANA zone name.
var ErrInvalidTimezone = localtime.ErrInvalidZone

// ErrInvalidTime means a local time is not HH:MM.
var ErrInvalidTime = localtime.ErrInvalidTime

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
		if err := localtime.ValidateZone(*settings.Timezone); err != nil {
			return Profile{}, err
		}
		params.Timezone = settings.Timezone
	}
	if settings.AllDayReminderTime != nil {
		t, err := localtime.Parse(*settings.AllDayReminderTime)
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
		AllDayReminderTime: localtime.Format(user.AllDayReminderTime),
		InboxListID:        inboxID,
	}, nil
}
