package httpapi

import (
	"context"

	"github.com/brusapa/brinketask/internal/tasks"
)

// CreateReminder adds a reminder to a task, or returns the existing one
// with 200 (D-04).
func (s Server) CreateReminder(ctx context.Context, request CreateReminderRequestObject) (CreateReminderResponseObject, error) {
	userID, err := callerID(ctx)
	if err != nil {
		return nil, err
	}
	reminder, created, err := s.tasks.CreateReminder(ctx, userID, request.Id, newReminderFromAPI(*request.Body))
	if problem, ok := domainProblem(err); ok {
		return CreateReminderdefaultApplicationProblemPlusJSONResponse(toResponse(problem)), nil
	}
	if err != nil {
		return nil, err
	}
	if created {
		return CreateReminder201JSONResponse(reminderToAPI(reminder)), nil
	}
	return CreateReminder200JSONResponse(reminderToAPI(reminder)), nil
}

// PatchReminder changes a reminder's offset or instant.
func (s Server) PatchReminder(ctx context.Context, request PatchReminderRequestObject) (PatchReminderResponseObject, error) {
	userID, err := callerID(ctx)
	if err != nil {
		return nil, err
	}
	reminder, err := s.tasks.PatchReminder(ctx, userID, request.Id, tasks.ReminderPatch{
		OffsetMinutes: request.Body.OffsetMinutes,
		At:            request.Body.At,
	})
	if problem, ok := domainProblem(err); ok {
		return PatchReminderdefaultApplicationProblemPlusJSONResponse(toResponse(problem)), nil
	}
	if err != nil {
		return nil, err
	}
	return PatchReminder200JSONResponse(reminderToAPI(reminder)), nil
}

// DeleteReminder soft-deletes a reminder.
func (s Server) DeleteReminder(ctx context.Context, request DeleteReminderRequestObject) (DeleteReminderResponseObject, error) {
	userID, err := callerID(ctx)
	if err != nil {
		return nil, err
	}
	err = s.tasks.DeleteReminder(ctx, userID, request.Id)
	if problem, ok := domainProblem(err); ok {
		return DeleteReminderdefaultApplicationProblemPlusJSONResponse(toResponse(problem)), nil
	}
	if err != nil {
		return nil, err
	}
	return DeleteReminder204Response{}, nil
}

// SnoozeTask creates a one-off snooze reminder (D-32).
func (s Server) SnoozeTask(ctx context.Context, request SnoozeTaskRequestObject) (SnoozeTaskResponseObject, error) {
	userID, err := callerID(ctx)
	if err != nil {
		return nil, err
	}
	reminder, created, err := s.tasks.Snooze(ctx, userID, request.Id, request.Body.ReminderId, request.Body.Until)
	if problem, ok := domainProblem(err); ok {
		return SnoozeTaskdefaultApplicationProblemPlusJSONResponse(toResponse(problem)), nil
	}
	if err != nil {
		return nil, err
	}
	if created {
		return SnoozeTask201JSONResponse(reminderToAPI(reminder)), nil
	}
	return SnoozeTask200JSONResponse(reminderToAPI(reminder)), nil
}
