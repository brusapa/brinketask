package httpapi

import (
	"context"

	"github.com/brusapa/brinketask/internal/tasks"
)

// CreateChecklistItem adds an item to a task, or returns the existing one
// with 200 (D-04).
func (s Server) CreateChecklistItem(ctx context.Context, request CreateChecklistItemRequestObject) (CreateChecklistItemResponseObject, error) {
	userID, err := callerID(ctx)
	if err != nil {
		return nil, err
	}
	body := request.Body
	item, created, err := s.tasks.CreateChecklistItem(ctx, userID, request.Id, tasks.NewChecklistItem{
		ID:       body.Id,
		Title:    body.Title,
		IsDone:   body.IsDone != nil && *body.IsDone,
		Position: body.Position,
	})
	if problem, ok := domainProblem(err); ok {
		return CreateChecklistItemdefaultApplicationProblemPlusJSONResponse(toResponse(problem)), nil
	}
	if err != nil {
		return nil, err
	}
	if created {
		return CreateChecklistItem201JSONResponse(checklistItemToAPI(item)), nil
	}
	return CreateChecklistItem200JSONResponse(checklistItemToAPI(item)), nil
}

// PatchChecklistItem applies a merge patch to an item.
func (s Server) PatchChecklistItem(ctx context.Context, request PatchChecklistItemRequestObject) (PatchChecklistItemResponseObject, error) {
	userID, err := callerID(ctx)
	if err != nil {
		return nil, err
	}
	item, err := s.tasks.PatchChecklistItem(ctx, userID, request.Id, tasks.ChecklistItemPatch{
		Title:    request.Body.Title,
		IsDone:   request.Body.IsDone,
		Position: request.Body.Position,
	})
	if problem, ok := domainProblem(err); ok {
		return PatchChecklistItemdefaultApplicationProblemPlusJSONResponse(toResponse(problem)), nil
	}
	if err != nil {
		return nil, err
	}
	return PatchChecklistItem200JSONResponse(checklistItemToAPI(item)), nil
}

// DeleteChecklistItem soft-deletes an item.
func (s Server) DeleteChecklistItem(ctx context.Context, request DeleteChecklistItemRequestObject) (DeleteChecklistItemResponseObject, error) {
	userID, err := callerID(ctx)
	if err != nil {
		return nil, err
	}
	err = s.tasks.DeleteChecklistItem(ctx, userID, request.Id)
	if problem, ok := domainProblem(err); ok {
		return DeleteChecklistItemdefaultApplicationProblemPlusJSONResponse(toResponse(problem)), nil
	}
	if err != nil {
		return nil, err
	}
	return DeleteChecklistItem204Response{}, nil
}
