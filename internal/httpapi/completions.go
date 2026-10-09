package httpapi

import (
	"context"

	"github.com/brusapa/brinketask/internal/tasks"
)

// CompleteTask completes a task (SPEC section 5, D-10, D-24).
func (s Server) CompleteTask(ctx context.Context, request CompleteTaskRequestObject) (CompleteTaskResponseObject, error) {
	userID, err := callerID(ctx)
	if err != nil {
		return nil, err
	}
	result, err := s.tasks.Complete(ctx, userID, request.Id, completeInput(request.Body))
	if problem, ok := domainProblem(err); ok {
		return CompleteTaskdefaultApplicationProblemPlusJSONResponse(toResponse(problem)), nil
	}
	if err != nil {
		return nil, err
	}
	return CompleteTask200JSONResponse(completionResultToAPI(result)), nil
}

// SkipTaskOccurrence skips the current occurrence of a recurring task
// (R-6, D-62). It takes the same body as /complete.
func (s Server) SkipTaskOccurrence(ctx context.Context, request SkipTaskOccurrenceRequestObject) (SkipTaskOccurrenceResponseObject, error) {
	userID, err := callerID(ctx)
	if err != nil {
		return nil, err
	}
	result, err := s.tasks.Skip(ctx, userID, request.Id, completeInput(request.Body))
	if problem, ok := domainProblem(err); ok {
		return SkipTaskOccurrencedefaultApplicationProblemPlusJSONResponse(toResponse(problem)), nil
	}
	if err != nil {
		return nil, err
	}
	return SkipTaskOccurrence200JSONResponse(completionResultToAPI(result)), nil
}

// UncompleteTask undoes the latest completion of a task.
func (s Server) UncompleteTask(ctx context.Context, request UncompleteTaskRequestObject) (UncompleteTaskResponseObject, error) {
	userID, err := callerID(ctx)
	if err != nil {
		return nil, err
	}
	result, err := s.tasks.Uncomplete(ctx, userID, request.Id, request.Body.CompletionId)
	if problem, ok := domainProblem(err); ok {
		return UncompleteTaskdefaultApplicationProblemPlusJSONResponse(toResponse(problem)), nil
	}
	if err != nil {
		return nil, err
	}
	return UncompleteTask200JSONResponse(completionResultToAPI(result)), nil
}

// ListTaskCompletions returns a task's completion history.
func (s Server) ListTaskCompletions(ctx context.Context, request ListTaskCompletionsRequestObject) (ListTaskCompletionsResponseObject, error) {
	userID, err := callerID(ctx)
	if err != nil {
		return nil, err
	}
	limit := defaultLimit
	if request.Params.Limit != nil {
		limit = *request.Params.Limit
	}
	records, next, err := s.tasks.TaskCompletions(ctx, userID, request.Id, request.Params.Cursor, limit)
	if problem, ok := domainProblem(err); ok {
		return ListTaskCompletionsdefaultApplicationProblemPlusJSONResponse(toResponse(problem)), nil
	}
	if err != nil {
		return nil, err
	}
	items := make([]Completion, len(records))
	for i, r := range records {
		items[i] = completionToAPI(r)
	}
	return ListTaskCompletions200JSONResponse{Items: items, NextCursor: pointerToNullable(next)}, nil
}

// ListCompletions feeds the Completed section (SPEC section 8).
func (s Server) ListCompletions(ctx context.Context, request ListCompletionsRequestObject) (ListCompletionsResponseObject, error) {
	userID, err := callerID(ctx)
	if err != nil {
		return nil, err
	}
	params := request.Params
	filter := tasks.CompletionFilter{
		CompletedFrom: params.CompletedFrom,
		CompletedTo:   params.CompletedTo,
		ListID:        params.ListId,
		TagID:         params.TagId,
		DueFrom:       dateFromAPI(params.DueFrom),
		DueTo:         dateFromAPI(params.DueTo),
		Cursor:        params.Cursor,
		Limit:         defaultLimit,
	}
	if params.Limit != nil {
		filter.Limit = *params.Limit
	}
	entries, next, err := s.tasks.Completions(ctx, userID, filter)
	if problem, ok := domainProblem(err); ok {
		return ListCompletionsdefaultApplicationProblemPlusJSONResponse(toResponse(problem)), nil
	}
	if err != nil {
		return nil, err
	}
	items := make([]CompletionEntry, len(entries))
	for i, e := range entries {
		items[i] = completionEntryToAPI(e)
	}
	return ListCompletions200JSONResponse{Items: items, NextCursor: pointerToNullable(next)}, nil
}

// completeInput reads the body shared by /complete and /skip.
func completeInput(body *CompleteRequest) tasks.CompleteInput {
	in := tasks.CompleteInput{
		CompletionID: body.CompletionId,
		CompletedAt:  body.CompletedAt,
	}
	if date := nullableToPointer(body.OccurrenceDueDate); date != nil {
		in.OccurrenceDueDate = dateFromAPI(date)
	}
	return in
}
