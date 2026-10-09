package httpapi

import (
	"context"
	"errors"

	"github.com/brusapa/brinketask/internal/tasks"
)

// GetChanges returns the caller's changes since a cursor, or the full live
// state without one (SPEC section 8).
func (s Server) GetChanges(ctx context.Context, request GetChangesRequestObject) (GetChangesResponseObject, error) {
	userID, err := callerID(ctx)
	if err != nil {
		return nil, err
	}
	limit := defaultLimit
	if request.Params.Limit != nil {
		limit = *request.Params.Limit
	}
	page, err := s.tasks.Changes(ctx, userID, request.Params.Cursor, limit)
	if problem, ok := domainProblem(err); ok {
		// 410 has its own response in the contract.
		if errors.Is(err, tasks.ErrCursorExpired) {
			return GetChanges410ApplicationProblemPlusJSONResponse(problem), nil
		}
		return GetChangesdefaultApplicationProblemPlusJSONResponse(toResponse(problem)), nil
	}
	if err != nil {
		return nil, err
	}

	// make() gives empty arrays rather than nil slices, which JSON would
	// send as null; the contract requires arrays.
	result := ChangesPage{
		Lists:          make([]List, len(page.Lists)),
		Tasks:          make([]Task, len(page.Tasks)),
		ChecklistItems: make([]ChecklistItem, len(page.ChecklistItems)),
		Reminders:      make([]Reminder, len(page.Reminders)),
		Tags:           make([]Tag, len(page.Tags)),
		NextCursor:     page.NextCursor,
		HasMore:        page.HasMore,
	}
	for i, l := range page.Lists {
		result.Lists[i] = listToAPI(l)
	}
	for i, t := range page.Tasks {
		// Tasks come without checklist and reminders; those have their own
		// arrays (contract, /sync/changes).
		result.Tasks[i] = taskToAPI(tasks.Task{Task: t}, false)
	}
	for i, c := range page.ChecklistItems {
		result.ChecklistItems[i] = checklistItemToAPI(c)
	}
	for i, r := range page.Reminders {
		result.Reminders[i] = reminderToAPI(r)
	}
	for i, t := range page.Tags {
		result.Tags[i] = tagToAPI(t)
	}
	return GetChanges200JSONResponse(result), nil
}
