package account_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/brusapa/brinketask/internal/account"
	"github.com/brusapa/brinketask/internal/clock"
	"github.com/brusapa/brinketask/internal/storage/storagetest"
)

func TestProfileDefaults(t *testing.T) {
	ctx := context.Background()
	pool := storagetest.NewPool(t)
	svc := account.NewService(pool, clock.NewFixed(start))
	userID, err := svc.SignIn(ctx, alice())
	if err != nil {
		t.Fatal(err)
	}

	p, err := svc.Profile(ctx, userID)
	if err != nil {
		t.Fatalf("Profile: %v", err)
	}
	var inboxID uuid.UUID
	if err := pool.QueryRow(ctx, "SELECT id FROM lists WHERE is_inbox").Scan(&inboxID); err != nil {
		t.Fatal(err)
	}
	if p.ID != userID || p.InboxListID != inboxID {
		t.Errorf("profile ids = %v, %v; want %v, %v", p.ID, p.InboxListID, userID, inboxID)
	}
	if p.Timezone != "Europe/Madrid" || p.AllDayReminderTime != "09:00" {
		t.Errorf("settings = %q, %q; want Europe/Madrid, 09:00", p.Timezone, p.AllDayReminderTime)
	}
	if p.Email == nil || *p.Email != "alice@example.com" || p.DisplayName == nil || *p.DisplayName != "Alice" {
		t.Errorf("claims = %v, %v", p.Email, p.DisplayName)
	}

	if _, err := svc.Profile(ctx, uuid.New()); !errors.Is(err, account.ErrNotFound) {
		t.Errorf("Profile(unknown) = %v, want ErrNotFound", err)
	}
}

func TestUpdateSettings(t *testing.T) {
	ctx := context.Background()
	pool := storagetest.NewPool(t)
	clk := clock.NewFixed(start)
	svc := account.NewService(pool, clk)
	userID, err := svc.SignIn(ctx, alice())
	if err != nil {
		t.Fatal(err)
	}
	updatedAt := func() time.Time {
		var at time.Time
		if err := pool.QueryRow(ctx, "SELECT updated_at FROM users").Scan(&at); err != nil {
			t.Fatal(err)
		}
		return at
	}

	// Only the fields sent change (D-05).
	clk.Advance(time.Hour)
	p, err := svc.UpdateSettings(ctx, userID, account.Settings{Timezone: ptr("America/New_York")})
	if err != nil {
		t.Fatalf("UpdateSettings: %v", err)
	}
	if p.Timezone != "America/New_York" || p.AllDayReminderTime != "09:00" {
		t.Errorf("after timezone patch: %q, %q", p.Timezone, p.AllDayReminderTime)
	}
	if !updatedAt().Equal(start.Add(time.Hour)) {
		t.Errorf("updated_at = %v, want %v", updatedAt(), start.Add(time.Hour))
	}

	clk.Advance(time.Hour)
	p, err = svc.UpdateSettings(ctx, userID, account.Settings{AllDayReminderTime: ptr("07:30")})
	if err != nil {
		t.Fatal(err)
	}
	if p.Timezone != "America/New_York" || p.AllDayReminderTime != "07:30" {
		t.Errorf("after time patch: %q, %q", p.Timezone, p.AllDayReminderTime)
	}

	// An empty patch, or one that repeats the current values, changes
	// nothing, not even updated_at.
	before := updatedAt()
	clk.Advance(time.Hour)
	if _, err := svc.UpdateSettings(ctx, userID, account.Settings{}); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.UpdateSettings(ctx, userID, account.Settings{Timezone: ptr("America/New_York")}); err != nil {
		t.Fatal(err)
	}
	if !updatedAt().Equal(before) {
		t.Errorf("updated_at moved from %v to %v without a change", before, updatedAt())
	}
}

func TestUpdateSettingsRejectsInvalidValues(t *testing.T) {
	ctx := context.Background()
	pool := storagetest.NewPool(t)
	svc := account.NewService(pool, clock.NewFixed(start))
	userID, err := svc.SignIn(ctx, alice())
	if err != nil {
		t.Fatal(err)
	}

	for _, zone := range []string{"Mars/Olympus_Mons", "Local", "", "Europe/../etc/passwd", "europe/madrid"} {
		_, err := svc.UpdateSettings(ctx, userID, account.Settings{Timezone: ptr(zone)})
		if !errors.Is(err, account.ErrInvalidTimezone) {
			t.Errorf("timezone %q: err = %v, want ErrInvalidTimezone", zone, err)
		}
	}
	for _, value := range []string{"24:00", "9:00", "09:60", "noon"} {
		_, err := svc.UpdateSettings(ctx, userID, account.Settings{AllDayReminderTime: ptr(value)})
		if !errors.Is(err, account.ErrInvalidTime) {
			t.Errorf("time %q: err = %v, want ErrInvalidTime", value, err)
		}
	}
	// A valid zone together with an invalid time changes nothing.
	_, err = svc.UpdateSettings(ctx, userID, account.Settings{Timezone: ptr("Asia/Tokyo"), AllDayReminderTime: ptr("25:00")})
	if !errors.Is(err, account.ErrInvalidTime) {
		t.Fatalf("err = %v, want ErrInvalidTime", err)
	}
	p, err := svc.Profile(ctx, userID)
	if err != nil {
		t.Fatal(err)
	}
	if p.Timezone != "Europe/Madrid" {
		t.Errorf("timezone = %q after a rejected patch, want unchanged", p.Timezone)
	}
}
