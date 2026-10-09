package tasks

import (
	"context"
	"fmt"
	"slices"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/oapi-codegen/nullable"

	"github.com/brusapa/brinketask/internal/localtime"
	"github.com/brusapa/brinketask/internal/storage/dbgen"
)

// NewTask is a task to create, with its first checklist items. The client
// generates the ids (D-03).
type NewTask struct {
	ID          uuid.UUID
	ListID      uuid.UUID
	Title       string
	Description string
	Priority    int16
	Position    string
	DueDate     *time.Time // a calendar date at UTC midnight
	DueTime     *string    // "HH:MM"
	DueTz       *string
	Rrule       *string
	RepeatFrom  string
	TagIDs      []uuid.UUID
	Checklist   []NewChecklistItem
	// HasReminders is set when the request carries reminders, which arrive
	// in phase 5 (D-34).
	HasReminders bool
}

// NewChecklistItem is a checklist item to create.
type NewChecklistItem struct {
	ID       uuid.UUID
	Title    string
	IsDone   bool
	Position string
}

// TaskPatch is a merge patch of a task (D-05): nil pointers and
// unspecified nullables leave their field alone; a specified null clears
// a nullable field.
type TaskPatch struct {
	ListID      *uuid.UUID
	Title       *string
	Description *string
	Status      *string
	Priority    *int16
	Position    *string
	DueDate     nullable.Nullable[time.Time]
	DueTime     nullable.Nullable[string]
	DueTz       nullable.Nullable[string]
	Rrule       nullable.Nullable[string]
	RepeatFrom  *string
	TagIDs      *[]uuid.UUID
}

// GetTask returns one of the caller's live tasks with its checklist.
func (s *Service) GetTask(ctx context.Context, userID, id uuid.UUID) (Task, error) {
	q := dbgen.New(s.pool)
	row, err := q.GetTaskForUser(ctx, dbgen.GetTaskForUserParams{UserID: userID, ID: id})
	if isNoRows(err) || (err == nil && row.DeletedAt != nil) {
		return Task{}, ErrNotFound
	}
	if err != nil {
		return Task{}, fmt.Errorf("tasks: get task: %w", err)
	}
	return withChecklist(ctx, q, row)
}

// CreateTask creates a task, open, at version 1, with its checklist items,
// in one transaction. created is false when the id already named one of
// the caller's tasks, which is returned as it is, even if deleted (D-04,
// D-23). Someone else's id is a conflict.
func (s *Service) CreateTask(ctx context.Context, userID uuid.UUID, in NewTask) (Task, bool, error) {
	return withRetry(func() (Task, bool, error) {
		var task Task
		created := false
		err := s.inTx(ctx, func(q *dbgen.Queries) error {
			existing, err := q.GetTaskForUser(ctx, dbgen.GetTaskForUserParams{UserID: userID, ID: in.ID})
			if err == nil {
				task, err = withChecklist(ctx, q, existing)
				return err
			}
			if !isNoRows(err) {
				return err
			}
			taken, err := q.TaskExists(ctx, in.ID)
			if err != nil {
				return err
			}
			if taken {
				return conflict("the id belongs to another user's task")
			}
			if in.Rrule != nil {
				return &NotImplementedError{Feature: "recurrence (rrule)"}
			}
			if in.HasReminders {
				return &NotImplementedError{Feature: "reminders"}
			}

			// Lock order: the target list and the tags before any task
			// (see DeleteList and DeleteTag).
			fields, err := lockTargetList(ctx, q, userID, in.ListID)
			if err != nil {
				return err
			}
			tagIDs, tagFields, err := lockTags(ctx, q, userID, in.TagIDs)
			if err != nil {
				return err
			}
			fields = append(fields, tagFields...)

			candidate := dbgen.Task{
				ID: in.ID, ListID: in.ListID, Title: in.Title, Description: in.Description,
				Status: StatusOpen, Priority: in.Priority, Position: in.Position,
				DueDate: in.DueDate, DueTz: in.DueTz, Rrule: in.Rrule, RepeatFrom: in.RepeatFrom, TagIds: tagIDs,
			}
			if in.DueTime != nil {
				parsed, err := localtime.Parse(*in.DueTime)
				if err != nil {
					fields = append(fields, FieldError{Field: "/due_time", Message: "not a HH:MM time"})
				}
				candidate.DueTime = parsed
			}
			fields = append(fields, checkDue(candidate)...)
			fields = append(fields, checkChecklistIDs(in.Checklist)...)
			if err := invalid(fields); err != nil {
				return err
			}
			for _, item := range in.Checklist {
				taken, err := q.ChecklistItemExists(ctx, item.ID)
				if err != nil {
					return err
				}
				if taken {
					return conflict("a checklist item id is already in use")
				}
			}

			now := s.now()
			seqs, err := reserveSeqs(ctx, q, 1+len(in.Checklist))
			if err != nil {
				return err
			}
			err = q.InsertTask(ctx, dbgen.InsertTaskParams{
				ID: candidate.ID, ListID: candidate.ListID, Title: candidate.Title,
				Description: candidate.Description, Priority: candidate.Priority, Position: candidate.Position,
				DueDate: candidate.DueDate, DueTime: candidate.DueTime, DueTz: candidate.DueTz,
				Rrule: candidate.Rrule, RepeatFrom: candidate.RepeatFrom, TagIds: candidate.TagIds,
				Seq: seqs[0], Now: now,
			})
			if err != nil {
				return err
			}
			for i, item := range in.Checklist {
				err := q.InsertChecklistItem(ctx, dbgen.InsertChecklistItemParams{
					ID: item.ID, TaskID: in.ID, Title: item.Title, IsDone: item.IsDone,
					Position: item.Position, Seq: seqs[1+i], Now: now,
				})
				if err != nil {
					return err
				}
			}

			row, err := q.GetTaskForUser(ctx, dbgen.GetTaskForUserParams{UserID: userID, ID: in.ID})
			if err != nil {
				return err
			}
			task, err = withChecklist(ctx, q, row)
			created = true
			return err
		})
		if err != nil {
			return Task{}, false, wrap("create task", err)
		}
		return task, created, nil
	})
}

// PatchTask applies a merge patch to one of the caller's live tasks.
//
// status may only go between open and dropped; a done task is reopened
// with uncomplete (D-25, D-38). The result must keep the due fields
// consistent (D-26). A patch that changes nothing writes nothing (D-41).
func (s *Service) PatchTask(ctx context.Context, userID, id uuid.UUID, patch TaskPatch) (Task, error) {
	if patch.Rrule.IsSpecified() && !patch.Rrule.IsNull() {
		return Task{}, &NotImplementedError{Feature: "recurrence (rrule)"}
	}
	var task Task
	err := s.inTx(ctx, func(q *dbgen.Queries) error {
		// Lock order: lists and tags before the task.
		var fields []FieldError
		if patch.ListID != nil {
			listFields, err := lockTargetList(ctx, q, userID, *patch.ListID)
			if err != nil {
				return err
			}
			fields = append(fields, listFields...)
		}
		var tagIDs []uuid.UUID
		if patch.TagIDs != nil {
			ids, tagFields, err := lockTags(ctx, q, userID, *patch.TagIDs)
			if err != nil {
				return err
			}
			tagIDs = ids
			fields = append(fields, tagFields...)
		}

		current, err := q.LockTaskForUser(ctx, dbgen.LockTaskForUserParams{UserID: userID, ID: id})
		if isNoRows(err) || (err == nil && current.DeletedAt != nil) {
			return ErrNotFound
		}
		if err != nil {
			return err
		}

		updated := current
		if patch.ListID != nil {
			updated.ListID = *patch.ListID
		}
		if patch.Title != nil {
			updated.Title = *patch.Title
		}
		if patch.Description != nil {
			updated.Description = *patch.Description
		}
		if patch.Priority != nil {
			updated.Priority = *patch.Priority
		}
		if patch.Position != nil {
			updated.Position = *patch.Position
		}
		if patch.RepeatFrom != nil {
			updated.RepeatFrom = *patch.RepeatFrom
		}
		if patch.TagIDs != nil {
			updated.TagIds = tagIDs
		}
		if patch.DueDate.IsSpecified() {
			updated.DueDate = nullableToPointer(patch.DueDate)
		}
		if patch.DueTime.IsSpecified() {
			updated.DueTime = pgtype.Time{}
			if !patch.DueTime.IsNull() {
				parsed, err := localtime.Parse(patch.DueTime.MustGet())
				if err != nil {
					fields = append(fields, FieldError{Field: "/due_time", Message: "not a HH:MM time"})
				}
				updated.DueTime = parsed
			}
		}
		if patch.DueTz.IsSpecified() {
			updated.DueTz = nullableToPointer(patch.DueTz)
		}
		if patch.Rrule.IsSpecified() {
			updated.Rrule = nil // only null reaches here (D-34)
		}
		if patch.Status != nil && *patch.Status != current.Status {
			if current.Status == StatusDone {
				return conflict("a completed task is reopened with uncomplete")
			}
			updated.Status = *patch.Status // open <-> dropped; the contract allows nothing else
		}

		fields = append(fields, checkDue(updated)...)
		if err := invalid(fields); err != nil {
			return err
		}
		if sameTask(updated, current) {
			task, err = withChecklist(ctx, q, current)
			return err
		}
		if err := s.writeTask(ctx, q, &updated); err != nil {
			return err
		}
		task, err = withChecklist(ctx, q, updated)
		return err
	})
	if err != nil {
		return Task{}, wrap("patch task", err)
	}
	return task, nil
}

// DeleteTask soft-deletes one of the caller's tasks. Its checklist items
// are left as they are and come back with it (D-45). Deleting a deleted
// task does nothing (D-22).
func (s *Service) DeleteTask(ctx context.Context, userID, id uuid.UUID) error {
	err := s.inTx(ctx, func(q *dbgen.Queries) error {
		current, err := q.LockTaskForUser(ctx, dbgen.LockTaskForUserParams{UserID: userID, ID: id})
		if isNoRows(err) {
			return ErrNotFound
		}
		if err != nil {
			return err
		}
		if current.DeletedAt != nil {
			return nil
		}
		now := s.now()
		current.DeletedAt = &now
		return s.writeTask(ctx, q, &current)
	})
	return wrap("delete task", err)
}

// RestoreTask undeletes one of the caller's tasks within the restore
// window. A task deleted together with its list comes back only with the
// list, and a task whose list is deleted cannot come back on its own (409).
// Restoring a live task returns it as it is.
func (s *Service) RestoreTask(ctx context.Context, userID, id uuid.UUID) (Task, error) {
	var task Task
	err := s.inTx(ctx, func(q *dbgen.Queries) error {
		current, err := q.LockTaskForUser(ctx, dbgen.LockTaskForUserParams{UserID: userID, ID: id})
		if isNoRows(err) {
			return ErrNotFound
		}
		if err != nil {
			return err
		}
		if current.DeletedAt == nil {
			task, err = withChecklist(ctx, q, current)
			return err
		}
		if current.DeletedWithListID != nil {
			return conflict("the task was deleted with its list; restore the list")
		}
		listLive, err := q.ListIsLive(ctx, current.ListID)
		if err != nil {
			return err
		}
		if !listLive {
			return conflict("the task's list is deleted")
		}
		if !restorable(*current.DeletedAt, s.now()) {
			return conflict("the task was deleted more than 30 days ago")
		}
		current.DeletedAt = nil
		if err := s.writeTask(ctx, q, &current); err != nil {
			return err
		}
		task, err = withChecklist(ctx, q, current)
		return err
	})
	if err != nil {
		return Task{}, wrap("restore task", err)
	}
	return task, nil
}

// lockTargetList checks that a task may be put in listID: a live list of
// the user (D-35), held with a share lock until the transaction ends.
func lockTargetList(ctx context.Context, q *dbgen.Queries, userID, listID uuid.UUID) ([]FieldError, error) {
	_, err := q.LockLiveListForShare(ctx, dbgen.LockLiveListForShareParams{UserID: userID, ID: listID})
	if isNoRows(err) {
		return []FieldError{{Field: "/list_id", Message: "not one of your lists"}}, nil
	}
	return nil, err
}

// lockTags removes duplicates from tagIDs (D-44) and checks that each is a
// live tag of the user (D-27), share-locking them until the transaction
// ends.
func lockTags(ctx context.Context, q *dbgen.Queries, userID uuid.UUID, tagIDs []uuid.UUID) ([]uuid.UUID, []FieldError, error) {
	unique := make([]uuid.UUID, 0, len(tagIDs))
	for _, id := range tagIDs {
		if !slices.Contains(unique, id) {
			unique = append(unique, id)
		}
	}
	if len(unique) == 0 {
		return unique, nil, nil
	}
	found, err := q.LockLiveTags(ctx, dbgen.LockLiveTagsParams{OwnerID: userID, Ids: unique})
	if err != nil {
		return nil, nil, err
	}
	if len(found) != len(unique) {
		return nil, []FieldError{{Field: "/tag_ids", Message: "contains an id that is not one of your tags"}}, nil
	}
	return unique, nil, nil
}

// checkDue enforces the dependencies between due fields (SPEC section 4,
// D-26) and that due_tz is a real zone.
func checkDue(t dbgen.Task) []FieldError {
	var fields []FieldError
	if t.DueTime.Valid && t.DueDate == nil {
		fields = append(fields, FieldError{Field: "/due_time", Message: "requires due_date"})
	}
	if t.DueTz != nil {
		if !t.DueTime.Valid {
			fields = append(fields, FieldError{Field: "/due_tz", Message: "requires due_time"})
		}
		if localtime.ValidateZone(*t.DueTz) != nil {
			fields = append(fields, FieldError{Field: "/due_tz", Message: "not an IANA time zone name"})
		}
	}
	if t.Rrule != nil && t.DueDate == nil {
		fields = append(fields, FieldError{Field: "/rrule", Message: "requires due_date"})
	}
	return fields
}

// checkChecklistIDs rejects a request that uses one item id twice.
func checkChecklistIDs(items []NewChecklistItem) []FieldError {
	seen := map[uuid.UUID]bool{}
	for _, item := range items {
		if seen[item.ID] {
			return []FieldError{{Field: "/checklist_items", Message: "an item id appears twice"}}
		}
		seen[item.ID] = true
	}
	return nil
}

// writeTask stores a changed task: one more version and a new seq (D-07).
func (s *Service) writeTask(ctx context.Context, q *dbgen.Queries, task *dbgen.Task) error {
	seq, err := q.NextSeq(ctx)
	if err != nil {
		return err
	}
	task.Version++
	task.Seq = seq
	task.UpdatedAt = s.now()
	return q.UpdateTask(ctx, dbgen.UpdateTaskParams{
		ID: task.ID, ListID: task.ListID, Title: task.Title, Description: task.Description,
		Status: task.Status, Priority: task.Priority, Position: task.Position,
		DueDate: task.DueDate, DueTime: task.DueTime, DueTz: task.DueTz, Rrule: task.Rrule,
		RecurrenceStart: task.RecurrenceStart, RepeatFrom: task.RepeatFrom, RecurrenceDoneCount: task.RecurrenceDoneCount,
		CompletedAt: task.CompletedAt, TagIds: task.TagIds, Version: task.Version, Seq: task.Seq,
		UpdatedAt: task.UpdatedAt, DeletedAt: task.DeletedAt,
	})
}

// sameTask compares the fields a patch can change.
func sameTask(a, b dbgen.Task) bool {
	return a.ListID == b.ListID && a.Title == b.Title && a.Description == b.Description &&
		a.Status == b.Status && a.Priority == b.Priority && a.Position == b.Position &&
		equalDates(a.DueDate, b.DueDate) && a.DueTime == b.DueTime &&
		equalPointers(a.DueTz, b.DueTz) && equalPointers(a.Rrule, b.Rrule) &&
		a.RepeatFrom == b.RepeatFrom && slices.Equal(a.TagIds, b.TagIds)
}

// equalDates compares two optional calendar dates.
func equalDates(a, b *time.Time) bool {
	if a == nil || b == nil {
		return a == b
	}
	return a.Equal(*b)
}

// withChecklist adds the live checklist items to a task row.
func withChecklist(ctx context.Context, q *dbgen.Queries, row dbgen.Task) (Task, error) {
	tasks, err := withChecklists(ctx, q, []dbgen.Task{row})
	if err != nil {
		return Task{}, err
	}
	return tasks[0], nil
}

// withChecklists adds the live checklist items to task rows with one query.
func withChecklists(ctx context.Context, q *dbgen.Queries, rows []dbgen.Task) ([]Task, error) {
	ids := make([]uuid.UUID, len(rows))
	for i, row := range rows {
		ids[i] = row.ID
	}
	items, err := q.LiveChecklistItemsForTasks(ctx, ids)
	if err != nil {
		return nil, err
	}
	byTask := map[uuid.UUID][]dbgen.ChecklistItem{}
	for _, item := range items {
		byTask[item.TaskID] = append(byTask[item.TaskID], item)
	}
	tasks := make([]Task, len(rows))
	for i, row := range rows {
		checklist := byTask[row.ID]
		if checklist == nil {
			checklist = []dbgen.ChecklistItem{} // an empty JSON array, not null
		}
		tasks[i] = Task{Task: row, ChecklistItems: checklist}
	}
	return tasks, nil
}
