package httpapi

import (
	"context"

	"github.com/brusapa/brinketask/internal/tasks"
)

// ListLists returns the caller's lists, or the trash with deleted=true.
func (s Server) ListLists(ctx context.Context, request ListListsRequestObject) (ListListsResponseObject, error) {
	userID, err := callerID(ctx)
	if err != nil {
		return nil, err
	}
	trash := request.Params.Deleted != nil && *request.Params.Deleted
	lists, err := s.tasks.Lists(ctx, userID, trash)
	if err != nil {
		return nil, err
	}
	items := make([]List, len(lists))
	for i, l := range lists {
		items[i] = listToAPI(l)
	}
	return ListLists200JSONResponse{Items: items}, nil
}

// CreateList creates a list, or returns the existing one with 200 (D-04).
func (s Server) CreateList(ctx context.Context, request CreateListRequestObject) (CreateListResponseObject, error) {
	userID, err := callerID(ctx)
	if err != nil {
		return nil, err
	}
	body := request.Body
	list, created, err := s.tasks.CreateList(ctx, userID, tasks.NewList{
		ID:       body.Id,
		Name:     body.Name,
		Color:    nullableToPointer(body.Color),
		Position: body.Position,
	})
	if problem, ok := domainProblem(err); ok {
		return CreateListdefaultApplicationProblemPlusJSONResponse(toResponse(problem)), nil
	}
	if err != nil {
		return nil, err
	}
	if created {
		return CreateList201JSONResponse(listToAPI(list)), nil
	}
	return CreateList200JSONResponse(listToAPI(list)), nil
}

// GetList returns one live list.
func (s Server) GetList(ctx context.Context, request GetListRequestObject) (GetListResponseObject, error) {
	userID, err := callerID(ctx)
	if err != nil {
		return nil, err
	}
	list, err := s.tasks.GetList(ctx, userID, request.Id)
	if problem, ok := domainProblem(err); ok {
		return GetListdefaultApplicationProblemPlusJSONResponse(toResponse(problem)), nil
	}
	if err != nil {
		return nil, err
	}
	return GetList200JSONResponse(listToAPI(list)), nil
}

// PatchList applies a merge patch to a list.
func (s Server) PatchList(ctx context.Context, request PatchListRequestObject) (PatchListResponseObject, error) {
	userID, err := callerID(ctx)
	if err != nil {
		return nil, err
	}
	body := request.Body
	list, err := s.tasks.PatchList(ctx, userID, request.Id, tasks.ListPatch{
		Name:     body.Name,
		Color:    body.Color,
		Position: body.Position,
	})
	if problem, ok := domainProblem(err); ok {
		return PatchListdefaultApplicationProblemPlusJSONResponse(toResponse(problem)), nil
	}
	if err != nil {
		return nil, err
	}
	return PatchList200JSONResponse(listToAPI(list)), nil
}

// DeleteList soft-deletes a list and its tasks.
func (s Server) DeleteList(ctx context.Context, request DeleteListRequestObject) (DeleteListResponseObject, error) {
	userID, err := callerID(ctx)
	if err != nil {
		return nil, err
	}
	err = s.tasks.DeleteList(ctx, userID, request.Id)
	if problem, ok := domainProblem(err); ok {
		return DeleteListdefaultApplicationProblemPlusJSONResponse(toResponse(problem)), nil
	}
	if err != nil {
		return nil, err
	}
	return DeleteList204Response{}, nil
}

// RestoreList undeletes a list and the tasks deleted with it.
func (s Server) RestoreList(ctx context.Context, request RestoreListRequestObject) (RestoreListResponseObject, error) {
	userID, err := callerID(ctx)
	if err != nil {
		return nil, err
	}
	list, err := s.tasks.RestoreList(ctx, userID, request.Id)
	if problem, ok := domainProblem(err); ok {
		return RestoreListdefaultApplicationProblemPlusJSONResponse(toResponse(problem)), nil
	}
	if err != nil {
		return nil, err
	}
	return RestoreList200JSONResponse(listToAPI(list)), nil
}
