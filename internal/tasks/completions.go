package tasks

import (
	"context"
	"fmt"
	"time"

	"github.com/google/uuid"

	"github.com/brusapa/brinketask/internal/recurrence"
	"github.com/brusapa/brinketask/internal/storage/dbgen"
)

// Kinds of completion record (SPEC section 4).
const (
	kindCompleted = "completed"
	kindSkipped   = "skipped"
)

// CompleteInput is the body of /complete.
type CompleteInput struct {
	CompletionID uuid.UUID
	// OccurrenceDueDate is the due date the client saw (D-10); nil when
	// the task had none.
	OccurrenceDueDate *time.Time
	// CompletedAt is when the user acted; nil means now.
	CompletedAt *time.Time
}

// CompletionResult is the outcome of complete or uncomplete: whether it
// changed anything, the task as it is now, and the record involved.
type CompletionResult struct {
	Applied    bool
	Task       Task
	Completion *Completion
}

// CompletionEntry is a record of the Completed section, with a summary of
// its task.
type CompletionEntry struct {
	Completion
	ListID   uuid.UUID
	Title    string
	Priority int16
	Rrule    *string
	CanUndo  bool
}

// CompletionFilter holds the filters of GET /completions. CompletedFrom
// and CompletedTo bound completed_at as [from, to).
type CompletionFilter struct {
	CompletedFrom time.Time
	CompletedTo   time.Time
	ListID        *uuid.UUID
	TagID         *uuid.UUID
	DueFrom       *time.Time
	DueTo         *time.Time
	Cursor        *string
	Limit         int
}

type completionsCursor struct {
	Kind        string    `json:"k"`
	CompletedAt time.Time `json:"c"`
	ID          uuid.UUID `json:"i"`
}

func (c completionsCursor) cursorKind() string { return c.Kind }

type historyCursor struct {
	Kind    string `json:"k"`
	TaskSeq int64  `json:"s"`
}

func (c historyCursor) cursorKind() string { return c.Kind }

const (
	cursorCompletions = "completions"
	cursorHistory     = "history"
)

// Complete records the completion of one of the caller's live tasks
// (SPEC section 5, "Complete and undo"): a task without a rule becomes
// done; a recurring task advances to its next occurrence, or becomes done
// at the end of its series (R-2 to R-5).
//
//   - A completion_id already recorded for this task answers applied=true
//     with that record and changes nothing (D-24); if that record was
//     undone, applied=false (D-39). An id recorded for another task is a
//     conflict.
//   - An occurrence_due_date that is not the task's due date is a stale
//     request: applied=false, nothing changes (D-10).
//   - A done task is a no-op (applied=false); a dropped task is a conflict
//     (D-37).
//   - completed_at defaults to now; a future value is clamped to now.
func (s *Service) Complete(ctx context.Context, userID, taskID uuid.UUID, in CompleteInput) (CompletionResult, error) {
	return s.record(ctx, userID, taskID, in, kindCompleted)
}

// Skip passes over the current occurrence of a recurring task (R-6): the
// same as Complete, with a "skipped" record, which counts towards COUNT
// and never shows in the Completed section. A task without a rule is a
// conflict (D-62).
func (s *Service) Skip(ctx context.Context, userID, taskID uuid.UUID, in CompleteInput) (CompletionResult, error) {
	return s.record(ctx, userID, taskID, in, kindSkipped)
}

// record is Complete and Skip; kind tells them apart.
func (s *Service) record(ctx context.Context, userID, taskID uuid.UUID, in CompleteInput, kind string) (CompletionResult, error) {
	var result CompletionResult
	err := s.inTx(ctx, func(q *dbgen.Queries) error {
		task, err := q.LockTaskForUser(ctx, dbgen.LockTaskForUserParams{UserID: userID, ID: taskID})
		if isNoRows(err) || (err == nil && task.DeletedAt != nil) {
			return ErrNotFound
		}
		if err != nil {
			return err
		}

		existing, err := q.GetCompletion(ctx, in.CompletionID)
		switch {
		case err == nil && existing.TaskID != taskID:
			return conflict("the completion id belongs to another task")
		case err == nil:
			result, err = s.unchanged(ctx, q, task, existing.UndoneAt == nil, &existing)
			return err
		case !isNoRows(err):
			return err
		}

		if kind == kindSkipped && task.Rrule == nil {
			return conflict("only an occurrence of a recurring task can be skipped")
		}
		switch task.Status {
		case StatusDone:
			result, err = s.unchanged(ctx, q, task, false, nil)
			return err
		case StatusDropped:
			return conflict("a dropped task must be reopened first")
		}
		if task.DueDate != nil && in.OccurrenceDueDate == nil {
			return invalid([]FieldError{{
				Field: "/occurrence_due_date", Message: "required when the task has a due date",
			}})
		}
		if !equalDates(task.DueDate, in.OccurrenceDueDate) {
			result, err = s.unchanged(ctx, q, task, false, nil)
			return err
		}

		now := s.now()
		completedAt := now
		if in.CompletedAt != nil && in.CompletedAt.Before(now) {
			completedAt = *in.CompletedAt
		}
		previous := task
		advanced := false
		if task.Rrule == nil {
			task.Status = StatusDone
			task.CompletedAt = &completedAt
		} else {
			advanced, err = s.advance(ctx, q, userID, &task, completedAt)
			if err != nil {
				return err
			}
		}
		if err := s.writeTask(ctx, q, &task); err != nil {
			return err
		}
		if advanced {
			// R-7: the next occurrence starts with an unticked checklist.
			if err := s.uncheckItems(ctx, q, task.ID); err != nil {
				return err
			}
		}
		record := dbgen.InsertCompletionParams{
			ID: in.CompletionID, TaskID: taskID, Kind: kind,
			OccurrenceDueDate: in.OccurrenceDueDate, CompletedAt: completedAt,
			PrevDueDate: previous.DueDate, PrevDueTime: previous.DueTime,
			PrevRecurrenceDoneCount: previous.RecurrenceDoneCount, PrevStatus: previous.Status,
			TaskSeq: task.Seq,
		}
		if err := q.InsertCompletion(ctx, record); err != nil {
			return err
		}
		completion, err := q.GetCompletion(ctx, in.CompletionID)
		if err != nil {
			return err
		}
		withItems, err := withChecklist(ctx, q, task)
		result = CompletionResult{Applied: true, Task: withItems, Completion: &completion}
		return err
	})
	if isUniqueViolation(err) {
		// The same completion id was used on another task at the same time.
		return CompletionResult{}, conflict("the completion id belongs to another task")
	}
	if err != nil {
		return CompletionResult{}, wrap(kind+" task", err)
	}
	return result, nil
}

// advance moves a recurring task past the occurrence just completed or
// skipped. It counts the occurrence (D-57); then either the series is over
// (COUNT reached, or the next date past UNTIL: R-5) and the task becomes
// done, or the due date moves to the next occurrence (R-2 or R-3). The due
// time and zone stay as they are (R-4). advanced reports the second case.
//
// "Today" and the completion date are calendar dates in the user's
// profile zone (SPEC section 5).
func (s *Service) advance(ctx context.Context, q *dbgen.Queries, userID uuid.UUID, task *dbgen.Task, completedAt time.Time) (advanced bool, err error) {
	rule, err := recurrence.Parse(*task.Rrule)
	if err != nil {
		// Stored rules were validated when written.
		return false, fmt.Errorf("stored rule %q: %w", *task.Rrule, err)
	}
	user, err := q.GetUser(ctx, userID)
	if err != nil {
		return false, err
	}
	zone, err := time.LoadLocation(user.Timezone)
	if err != nil {
		return false, fmt.Errorf("user time zone: %w", err)
	}

	task.RecurrenceDoneCount++
	var next time.Time
	ok := false
	switch {
	case rule.Count != 0 && int(task.RecurrenceDoneCount) >= rule.Count:
		// R-5: the last occurrence of a COUNT series.
	case task.RepeatFrom == RepeatFromCompletion:
		next, ok = recurrence.NextFromCompletion(rule, dateOf(completedAt, zone))
	default:
		start := task.DueDate
		if task.RecurrenceStart != nil {
			start = task.RecurrenceStart
		}
		next, ok = recurrence.NextDue(rule, *start, *task.DueDate, dateOf(s.now(), zone))
	}
	if !ok {
		task.Status = StatusDone
		task.CompletedAt = &completedAt
		return false, nil
	}
	task.DueDate = &next
	return true, nil
}

// dateOf is the calendar date of an instant in a zone, at midnight UTC as
// due dates are stored.
func dateOf(instant time.Time, zone *time.Location) time.Time {
	year, month, day := instant.In(zone).Date()
	return time.Date(year, month, day, 0, 0, 0, 0, time.UTC)
}

// uncheckItems unticks the live checklist items of a task, each one a
// write with its own seq (D-07). The task's lock is held, so its items are
// locked after it, as everywhere else.
func (s *Service) uncheckItems(ctx context.Context, q *dbgen.Queries, taskID uuid.UUID) error {
	ids, err := q.LockDoneItemIDsOfTask(ctx, taskID)
	if err != nil || len(ids) == 0 {
		return err
	}
	seqs, err := reserveSeqs(ctx, q, len(ids))
	if err != nil {
		return err
	}
	return q.UncheckItems(ctx, dbgen.UncheckItemsParams{Ids: ids, Seqs: seqs, Now: s.now()})
}

// Uncomplete undoes a completion record of one of the caller's live tasks,
// restoring the state saved in it, even if the due date changed since
// (D-25). Only the latest record not undone can be undone (409 otherwise).
// An unknown or already undone completion_id is a no-op (applied=false).
func (s *Service) Uncomplete(ctx context.Context, userID, taskID, completionID uuid.UUID) (CompletionResult, error) {
	var result CompletionResult
	err := s.inTx(ctx, func(q *dbgen.Queries) error {
		task, err := q.LockTaskForUser(ctx, dbgen.LockTaskForUserParams{UserID: userID, ID: taskID})
		if isNoRows(err) || (err == nil && task.DeletedAt != nil) {
			return ErrNotFound
		}
		if err != nil {
			return err
		}

		record, err := q.GetCompletion(ctx, completionID)
		if isNoRows(err) || (err == nil && (record.TaskID != taskID || record.UndoneAt != nil)) {
			result, err = s.unchanged(ctx, q, task, false, nil)
			return err
		}
		if err != nil {
			return err
		}
		latest, err := q.LatestCompletionID(ctx, taskID)
		if err != nil {
			return err
		}
		if latest != completionID {
			return conflict("only the latest completion of a task can be undone")
		}

		task.Status = record.PrevStatus
		if task.Status != StatusDone {
			task.CompletedAt = nil
		}
		task.DueDate = record.PrevDueDate
		task.DueTime = record.PrevDueTime
		task.RecurrenceDoneCount = record.PrevRecurrenceDoneCount
		if !task.DueTime.Valid {
			// The zone needs a time (SPEC section 4); a time added after
			// completing is gone again, so its zone goes too.
			task.DueTz = nil
		}
		if err := s.writeTask(ctx, q, &task); err != nil {
			return err
		}
		now := s.now()
		if err := q.MarkCompletionUndone(ctx, dbgen.MarkCompletionUndoneParams{ID: completionID, Now: &now}); err != nil {
			return err
		}
		record.UndoneAt = &now
		withItems, err := withChecklist(ctx, q, task)
		result = CompletionResult{Applied: true, Task: withItems, Completion: &record}
		return err
	})
	if err != nil {
		return CompletionResult{}, wrap("uncomplete task", err)
	}
	return result, nil
}

// unchanged builds the answer of a call that changes nothing.
func (s *Service) unchanged(ctx context.Context, q *dbgen.Queries, task dbgen.Task, applied bool, record *Completion) (CompletionResult, error) {
	withItems, err := withChecklist(ctx, q, task)
	if err != nil {
		return CompletionResult{}, err
	}
	return CompletionResult{Applied: applied, Task: withItems, Completion: record}, nil
}

// TaskCompletions returns the history of one of the caller's live tasks,
// newest first, without undone records (D-39).
func (s *Service) TaskCompletions(ctx context.Context, userID, taskID uuid.UUID, cursor *string, limit int) ([]Completion, *string, error) {
	q := dbgen.New(s.pool)
	task, err := q.GetTaskForUser(ctx, dbgen.GetTaskForUserParams{UserID: userID, ID: taskID})
	if isNoRows(err) || (err == nil && task.DeletedAt != nil) {
		return nil, nil, ErrNotFound
	}
	if err != nil {
		return nil, nil, fmt.Errorf("tasks: completion history: %w", err)
	}
	params := dbgen.ListTaskCompletionsParams{TaskID: taskID, Lim: int32(limit) + 1} //nolint:gosec // G115: limit <= 500
	if cursor != nil {
		var c historyCursor
		if err := decodeCursor(*cursor, cursorHistory, &c); err != nil {
			return nil, nil, err
		}
		params.BeforeSeq = &c.TaskSeq
	}
	records, err := q.ListTaskCompletions(ctx, params)
	if err != nil {
		return nil, nil, fmt.Errorf("tasks: completion history: %w", err)
	}
	var next *string
	if len(records) > limit {
		records = records[:limit]
		c := encodeCursor(historyCursor{Kind: cursorHistory, TaskSeq: records[limit-1].TaskSeq})
		next = &c
	}
	return records, next, nil
}

// Completions returns the Completed section (SPEC section 8): completed,
// not undone records of the caller's live tasks in [from, to), newest
// first, filtered like the view they belong to. The server applies no
// calendar logic: the client sends the bounds of the user's day.
func (s *Service) Completions(ctx context.Context, userID uuid.UUID, filter CompletionFilter) ([]CompletionEntry, *string, error) {
	q := dbgen.New(s.pool)
	if err := s.checkFilters(ctx, q, userID, filter.ListID, filter.TagID); err != nil {
		return nil, nil, err
	}
	params := dbgen.ListCompletionsParams{
		UserID: userID, CompletedFrom: filter.CompletedFrom, CompletedTo: filter.CompletedTo,
		ListID: filter.ListID, TagID: filter.TagID, DueFrom: filter.DueFrom, DueTo: filter.DueTo,
		Lim: int32(filter.Limit) + 1, //nolint:gosec // G115: limit <= 500
	}
	if filter.Cursor != nil {
		var c completionsCursor
		if err := decodeCursor(*filter.Cursor, cursorCompletions, &c); err != nil {
			return nil, nil, err
		}
		params.HasAfter, params.AfterCompletedAt, params.AfterID = true, c.CompletedAt, c.ID
	}
	rows, err := q.ListCompletions(ctx, params)
	if err != nil {
		return nil, nil, fmt.Errorf("tasks: completions: %w", err)
	}
	var next *string
	if len(rows) > filter.Limit {
		rows = rows[:filter.Limit]
		last := rows[len(rows)-1].TaskCompletion
		c := encodeCursor(completionsCursor{Kind: cursorCompletions, CompletedAt: last.CompletedAt, ID: last.ID})
		next = &c
	}
	entries := make([]CompletionEntry, len(rows))
	for i, row := range rows {
		entries[i] = CompletionEntry{
			Completion: row.TaskCompletion, ListID: row.ListID, Title: row.Title,
			Priority: row.Priority, Rrule: row.Rrule, CanUndo: row.CanUndo,
		}
	}
	return entries, next, nil
}
