package httpapi

import (
	"context"

	"github.com/brusapa/brinketask/internal/tasks"
)

// ListTags returns the caller's live tags.
func (s Server) ListTags(ctx context.Context, _ ListTagsRequestObject) (ListTagsResponseObject, error) {
	userID, err := callerID(ctx)
	if err != nil {
		return nil, err
	}
	tags, err := s.tasks.Tags(ctx, userID)
	if err != nil {
		return nil, err
	}
	items := make([]Tag, len(tags))
	for i, t := range tags {
		items[i] = tagToAPI(t)
	}
	return ListTags200JSONResponse{Items: items}, nil
}

// CreateTag creates a tag, or returns the existing one with 200 (D-04).
func (s Server) CreateTag(ctx context.Context, request CreateTagRequestObject) (CreateTagResponseObject, error) {
	userID, err := callerID(ctx)
	if err != nil {
		return nil, err
	}
	tag, created, err := s.tasks.CreateTag(ctx, userID, tasks.NewTag{
		ID:    request.Body.Id,
		Name:  request.Body.Name,
		Color: nullableToPointer(request.Body.Color),
	})
	if problem, ok := domainProblem(err); ok {
		return CreateTagdefaultApplicationProblemPlusJSONResponse(toResponse(problem)), nil
	}
	if err != nil {
		return nil, err
	}
	if created {
		return CreateTag201JSONResponse(tagToAPI(tag)), nil
	}
	return CreateTag200JSONResponse(tagToAPI(tag)), nil
}

// PatchTag applies a merge patch to a tag.
func (s Server) PatchTag(ctx context.Context, request PatchTagRequestObject) (PatchTagResponseObject, error) {
	userID, err := callerID(ctx)
	if err != nil {
		return nil, err
	}
	tag, err := s.tasks.PatchTag(ctx, userID, request.Id, tasks.TagPatch{
		Name:  request.Body.Name,
		Color: request.Body.Color,
	})
	if problem, ok := domainProblem(err); ok {
		return PatchTagdefaultApplicationProblemPlusJSONResponse(toResponse(problem)), nil
	}
	if err != nil {
		return nil, err
	}
	return PatchTag200JSONResponse(tagToAPI(tag)), nil
}

// DeleteTag soft-deletes a tag and removes it from the caller's tasks.
func (s Server) DeleteTag(ctx context.Context, request DeleteTagRequestObject) (DeleteTagResponseObject, error) {
	userID, err := callerID(ctx)
	if err != nil {
		return nil, err
	}
	err = s.tasks.DeleteTag(ctx, userID, request.Id)
	if problem, ok := domainProblem(err); ok {
		return DeleteTagdefaultApplicationProblemPlusJSONResponse(toResponse(problem)), nil
	}
	if err != nil {
		return nil, err
	}
	return DeleteTag204Response{}, nil
}
