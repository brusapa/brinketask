package httpapi

import (
	"context"
	"errors"

	"github.com/google/uuid"

	"github.com/brusapa/brinketask/internal/account"
)

// Accounts is what the /me operations need from package account.
// *account.Service implements it.
type Accounts interface {
	Profile(ctx context.Context, userID uuid.UUID) (account.Profile, error)
	UpdateSettings(ctx context.Context, userID uuid.UUID, settings account.Settings) (account.Profile, error)
}

// errNoSession means an operation ran without Authenticate in front of it,
// which is a wiring bug, so it answers 500.
var errNoSession = errors.New("httpapi: no session in the request context")

// GetMe returns the caller's profile.
func (s Server) GetMe(ctx context.Context, _ GetMeRequestObject) (GetMeResponseObject, error) {
	caller, ok := sessionFrom(ctx)
	if !ok {
		return nil, errNoSession
	}
	profile, err := s.accounts.Profile(ctx, caller.UserID)
	if err != nil {
		return nil, err
	}
	return GetMe200JSONResponse(userFrom(profile)), nil
}

// PatchMe updates the caller's settings. The contract has already checked
// the shape of the body (types, HH:MM, no other fields, no nulls); the
// account service checks that the time zone exists.
func (s Server) PatchMe(ctx context.Context, request PatchMeRequestObject) (PatchMeResponseObject, error) {
	caller, ok := sessionFrom(ctx)
	if !ok {
		return nil, errNoSession
	}
	settings := account.Settings{
		Timezone:           request.Body.Timezone,
		AllDayReminderTime: request.Body.AllDayReminderTime,
	}
	profile, err := s.accounts.UpdateSettings(ctx, caller.UserID, settings)
	switch {
	case errors.Is(err, account.ErrInvalidTimezone):
		return PatchMedefaultApplicationProblemPlusJSONResponse(toResponse(newValidationProblem([]FieldError{
			{Field: "/timezone", Message: "not an IANA time zone name"},
		}))), nil
	case errors.Is(err, account.ErrInvalidTime):
		return PatchMedefaultApplicationProblemPlusJSONResponse(toResponse(newValidationProblem([]FieldError{
			{Field: "/all_day_reminder_time", Message: "not a HH:MM time"},
		}))), nil
	case err != nil:
		return nil, err
	}
	return PatchMe200JSONResponse(userFrom(profile)), nil
}

// userFrom converts a profile to the contract's User. Absent claims are
// sent as an explicit null.
func userFrom(p account.Profile) User {
	return User{
		Id:                 p.ID,
		Email:              pointerToNullable(p.Email),
		DisplayName:        pointerToNullable(p.DisplayName),
		Timezone:           p.Timezone,
		AllDayReminderTime: p.AllDayReminderTime,
		InboxListId:        p.InboxListID,
	}
}
