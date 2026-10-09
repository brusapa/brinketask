package httpapi

import (
	"context"

	"github.com/brusapa/brinketask/internal/tasks"
)

// GetTask returns one live task with its checklist.
func (s Server) GetTask(ctx context.Context, request GetTaskRequestObject) (GetTaskResponseObject, error) {
	userID, err := callerID(ctx)
	if err != nil {
		return nil, err
	}
	task, err := s.tasks.GetTask(ctx, userID, request.Id)
	if problem, ok := domainProblem(err); ok {
		return GetTaskdefaultApplicationProblemPlusJSONResponse(toResponse(problem)), nil
	}
	if err != nil {
		return nil, err
	}
	return GetTask200JSONResponse(taskToAPI(task, true)), nil
}

// CreateTask creates a task with its checklist, or returns the existing one
// with 200 (D-04).
func (s Server) CreateTask(ctx context.Context, request CreateTaskRequestObject) (CreateTaskResponseObject, error) {
	userID, err := callerID(ctx)
	if err != nil {
		return nil, err
	}
	task, created, err := s.tasks.CreateTask(ctx, userID, newTaskFromAPI(request.Body))
	if problem, ok := domainProblem(err); ok {
		return CreateTaskdefaultApplicationProblemPlusJSONResponse(toResponse(problem)), nil
	}
	if err != nil {
		return nil, err
	}
	if created {
		return CreateTask201JSONResponse(taskToAPI(task, true)), nil
	}
	return CreateTask200JSONResponse(taskToAPI(task, true)), nil
}

// newTaskFromAPI applies the contract's defaults to optional fields.
func newTaskFromAPI(body *TaskCreate) tasks.NewTask {
	in := tasks.NewTask{
		ID:         body.Id,
		ListID:     body.ListId,
		Title:      body.Title,
		Position:   body.Position,
		RepeatFrom: string(RepeatFromDue),
		DueTime:    nullableToPointer(body.DueTime),
		DueTz:      nullableToPointer(body.DueTz),
		Rrule:      nullableToPointer(body.Rrule),
	}
	if body.Reminders != nil {
		for _, r := range *body.Reminders {
			in.Reminders = append(in.Reminders, newReminderFromAPI(r))
		}
	}
	if date := nullableToPointer(body.DueDate); date != nil {
		in.DueDate = dateFromAPI(date)
	}
	if body.Description != nil {
		in.Description = *body.Description
	}
	if body.Priority != nil {
		// The contract limits priority to 0..3, so it fits in an int16.
		in.Priority = int16(*body.Priority) //nolint:gosec // G115: range checked by the contract
	}
	if body.RepeatFrom != nil {
		in.RepeatFrom = string(*body.RepeatFrom)
	}
	if body.TagIds != nil {
		in.TagIDs = *body.TagIds
	}
	if body.ChecklistItems != nil {
		for _, item := range *body.ChecklistItems {
			in.Checklist = append(in.Checklist, tasks.NewChecklistItem{
				ID:       item.Id,
				Title:    item.Title,
				IsDone:   item.IsDone != nil && *item.IsDone,
				Position: item.Position,
			})
		}
	}
	return in
}

// PatchTask applies a merge patch to a task.
func (s Server) PatchTask(ctx context.Context, request PatchTaskRequestObject) (PatchTaskResponseObject, error) {
	userID, err := callerID(ctx)
	if err != nil {
		return nil, err
	}
	body := request.Body
	patch := tasks.TaskPatch{
		ListID:      body.ListId,
		Title:       body.Title,
		Description: body.Description,
		Position:    body.Position,
		DueDate:     nullableDateFromAPI(body.DueDate),
		DueTime:     body.DueTime,
		DueTz:       body.DueTz,
		Rrule:       body.Rrule,
		TagIDs:      body.TagIds,
	}
	if body.Status != nil {
		status := string(*body.Status)
		patch.Status = &status
	}
	if body.Priority != nil {
		priority := int16(*body.Priority) //nolint:gosec // G115: range checked by the contract
		patch.Priority = &priority
	}
	if body.RepeatFrom != nil {
		repeatFrom := string(*body.RepeatFrom)
		patch.RepeatFrom = &repeatFrom
	}
	task, err := s.tasks.PatchTask(ctx, userID, request.Id, patch)
	if problem, ok := domainProblem(err); ok {
		return PatchTaskdefaultApplicationProblemPlusJSONResponse(toResponse(problem)), nil
	}
	if err != nil {
		return nil, err
	}
	return PatchTask200JSONResponse(taskToAPI(task, true)), nil
}

// DeleteTask soft-deletes a task.
func (s Server) DeleteTask(ctx context.Context, request DeleteTaskRequestObject) (DeleteTaskResponseObject, error) {
	userID, err := callerID(ctx)
	if err != nil {
		return nil, err
	}
	err = s.tasks.DeleteTask(ctx, userID, request.Id)
	if problem, ok := domainProblem(err); ok {
		return DeleteTaskdefaultApplicationProblemPlusJSONResponse(toResponse(problem)), nil
	}
	if err != nil {
		return nil, err
	}
	return DeleteTask204Response{}, nil
}

// RestoreTask undeletes a task.
func (s Server) RestoreTask(ctx context.Context, request RestoreTaskRequestObject) (RestoreTaskResponseObject, error) {
	userID, err := callerID(ctx)
	if err != nil {
		return nil, err
	}
	task, err := s.tasks.RestoreTask(ctx, userID, request.Id)
	if problem, ok := domainProblem(err); ok {
		return RestoreTaskdefaultApplicationProblemPlusJSONResponse(toResponse(problem)), nil
	}
	if err != nil {
		return nil, err
	}
	return RestoreTask200JSONResponse(taskToAPI(task, true)), nil
}

// defaultLimit is the contract's default page size.
const defaultLimit = 100

// ListTasks returns one page of the caller's tasks.
func (s Server) ListTasks(ctx context.Context, request ListTasksRequestObject) (ListTasksResponseObject, error) {
	userID, err := callerID(ctx)
	if err != nil {
		return nil, err
	}
	params := request.Params
	query := tasks.TaskQuery{
		ListID:    params.ListId,
		TagID:     params.TagId,
		DueFrom:   dateFromAPI(params.DueFrom),
		DueTo:     dateFromAPI(params.DueTo),
		Search:    params.Q,
		SortByDue: params.Sort != nil && *params.Sort == ListTasksParamsSortDue,
		Trash:     params.Deleted != nil && *params.Deleted,
		Cursor:    params.Cursor,
		Limit:     defaultLimit,
	}
	if params.Status != nil {
		status := string(*params.Status)
		query.Status = &status
	}
	if params.Limit != nil {
		query.Limit = *params.Limit
	}
	found, next, err := s.tasks.QueryTasks(ctx, userID, query)
	if problem, ok := domainProblem(err); ok {
		return ListTasksdefaultApplicationProblemPlusJSONResponse(toResponse(problem)), nil
	}
	if err != nil {
		return nil, err
	}
	items := make([]Task, len(found))
	for i, t := range found {
		items[i] = taskToAPI(t, true)
	}
	return ListTasks200JSONResponse{Items: items, NextCursor: pointerToNullable(next)}, nil
}
