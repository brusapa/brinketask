package tasks

import (
	"context"
	"fmt"
	"time"

	"github.com/google/uuid"

	"github.com/brusapa/brinketask/internal/localtime"
	"github.com/brusapa/brinketask/internal/reminder"
	"github.com/brusapa/brinketask/internal/storage/dbgen"
)

// Reminder is the row as it is.
type Reminder = dbgen.Reminder

// MaxReminders is the most live reminders a task may have; snooze ones do
// not count (SPEC section 4).
const MaxReminders = 5

// NewReminder is a reminder to create. Exactly one of OffsetMinutes
// (relative) and At (absolute) is set; the client generates the id (D-03).
type NewReminder struct {
	ID            uuid.UUID
	Kind          string
	OffsetMinutes *int
	At            *time.Time
}

// ReminderPatch is a merge patch of a reminder (D-05). Its kind cannot
// change; the field sent must be the one of its kind.
type ReminderPatch struct {
	OffsetMinutes *int
	At            *time.Time
}

var errTooManyReminders = &ConflictError{Reason: fmt.Sprintf("a task has at most %d reminders", MaxReminders)}

// CreateReminder adds a reminder to one of the caller's live tasks.
// created is false when the id already named one of the caller's
// reminders, which is returned as it is (D-04, D-23).
func (s *Service) CreateReminder(ctx context.Context, userID, taskID uuid.UUID, in NewReminder) (Reminder, bool, error) {
	return withRetry(func() (Reminder, bool, error) {
		var result Reminder
		created := false
		err := s.inTx(ctx, func(q *dbgen.Queries) error {
			task, err := q.LockTaskForUser(ctx, dbgen.LockTaskForUserParams{UserID: userID, ID: taskID})
			if isNoRows(err) || (err == nil && task.DeletedAt != nil) {
				return ErrNotFound
			}
			if err != nil {
				return err
			}
			existing, found, err := s.existingReminder(ctx, q, userID, in.ID)
			if err != nil || found {
				result = existing
				return err
			}
			count, err := q.CountLiveRemindersOfTask(ctx, taskID)
			if err != nil {
				return err
			}
			if count >= MaxReminders {
				return errTooManyReminders
			}
			result, err = s.insertReminder(ctx, q, userID, task, in, "")
			created = err == nil
			return err
		})
		if err != nil {
			return Reminder{}, false, wrap("create reminder", err)
		}
		return result, created, nil
	})
}

// Snooze adds a one-off "snooze" reminder at until (D-32). It is idempotent
// on the reminder id; until must be in the future (422). Snooze reminders
// do not count towards the limit.
func (s *Service) Snooze(ctx context.Context, userID, taskID, reminderID uuid.UUID, until time.Time) (Reminder, bool, error) {
	return withRetry(func() (Reminder, bool, error) {
		var result Reminder
		created := false
		err := s.inTx(ctx, func(q *dbgen.Queries) error {
			task, err := q.LockTaskForUser(ctx, dbgen.LockTaskForUserParams{UserID: userID, ID: taskID})
			if isNoRows(err) || (err == nil && task.DeletedAt != nil) {
				return ErrNotFound
			}
			if err != nil {
				return err
			}
			existing, found, err := s.existingReminder(ctx, q, userID, reminderID)
			if err != nil || found {
				result = existing
				return err
			}
			if !until.After(s.now()) {
				return invalid([]FieldError{{Field: "/until", Message: "must be in the future"}})
			}
			result, err = s.insertReminder(ctx, q, userID, task,
				NewReminder{ID: reminderID, Kind: reminder.KindSnooze, At: &until}, "/until")
			created = err == nil
			return err
		})
		if err != nil {
			return Reminder{}, false, wrap("snooze", err)
		}
		return result, created, nil
	})
}

// existingReminder implements D-23 for reminders: the caller's own id is
// found (and returned as it is), someone else's is a conflict.
func (s *Service) existingReminder(ctx context.Context, q *dbgen.Queries, userID, id uuid.UUID) (Reminder, bool, error) {
	row, err := q.GetReminderForUser(ctx, dbgen.GetReminderForUserParams{UserID: userID, ID: id})
	if err == nil {
		return row.Reminder, true, nil
	}
	if !isNoRows(err) {
		return Reminder{}, false, err
	}
	taken, err := q.ReminderExists(ctx, id)
	if err != nil {
		return Reminder{}, false, err
	}
	if taken {
		return Reminder{}, false, conflict("the id belongs to another user's reminder")
	}
	return Reminder{}, false, nil
}

// insertReminder validates and stores a new reminder of a locked task.
// fieldPrefix names the request field of the instant in errors ("" for the
// reminder bodies, "/until" for snooze).
func (s *Service) insertReminder(ctx context.Context, q *dbgen.Queries, userID uuid.UUID, task dbgen.Task, in NewReminder, fieldPrefix string) (Reminder, error) {
	if fields := checkReminderFields(in.Kind, in.OffsetMinutes, in.At, task, fieldPrefix); len(fields) > 0 {
		return Reminder{}, &ValidationError{Fields: fields}
	}
	user, err := s.userSettings(ctx, q, userID)
	if err != nil {
		return Reminder{}, err
	}
	row := dbgen.Reminder{ID: in.ID, TaskID: task.ID, Kind: in.Kind, At: in.At}
	if in.OffsetMinutes != nil {
		offset := int32(*in.OffsetMinutes) //nolint:gosec // G115: 0 to 40320, checked above
		row.OffsetMinutes = &offset
	}
	row.NextFireAt = reminder.NextFire(asComputation(row), taskForComputation(task), user, s.now())
	seq, err := q.NextSeq(ctx)
	if err != nil {
		return Reminder{}, err
	}
	now := s.now()
	err = q.InsertReminder(ctx, dbgen.InsertReminderParams{
		ID: row.ID, TaskID: row.TaskID, Kind: row.Kind, OffsetMinutes: row.OffsetMinutes, At: row.At,
		NextFireAt: row.NextFireAt, Seq: seq, Now: now,
	})
	if err != nil {
		return Reminder{}, err
	}
	row.Version, row.Seq, row.CreatedAt, row.UpdatedAt = 1, seq, now, now
	return row, nil
}

// checkReminderFields enforces the fields of each kind (SPEC section 4) and
// that a relative reminder has a due date to count from (section 6).
func checkReminderFields(kind string, offset *int, at *time.Time, task dbgen.Task, atField string) []FieldError {
	if atField == "" {
		atField = "/at"
	}
	switch kind {
	case reminder.KindRelative:
		if offset == nil || at != nil {
			return []FieldError{{Field: "/offset_minutes", Message: "a relative reminder takes offset_minutes and no at"}}
		}
		if task.DueDate == nil {
			return []FieldError{{Field: "/offset_minutes", Message: "a relative reminder needs a task with a due date"}}
		}
	default: // absolute, snooze
		if at == nil || offset != nil {
			return []FieldError{{Field: atField, Message: "this reminder takes at and no offset_minutes"}}
		}
	}
	return nil
}

// PatchReminder changes the offset of a relative reminder or the instant
// of an absolute or snooze one.
func (s *Service) PatchReminder(ctx context.Context, userID, id uuid.UUID, patch ReminderPatch) (Reminder, error) {
	var result Reminder
	err := s.inTx(ctx, func(q *dbgen.Queries) error {
		row, err := q.LockReminderForUser(ctx, dbgen.LockReminderForUserParams{UserID: userID, ID: id})
		if isNoRows(err) || (err == nil && (row.Reminder.DeletedAt != nil || row.TaskDeleted)) {
			return ErrNotFound
		}
		if err != nil {
			return err
		}
		current := row.Reminder
		updated := current
		switch {
		case current.Kind == reminder.KindRelative && patch.At != nil,
			current.Kind != reminder.KindRelative && patch.OffsetMinutes != nil:
			field := "/at"
			if patch.OffsetMinutes != nil {
				field = "/offset_minutes"
			}
			return invalid([]FieldError{{Field: field, Message: "not a field of a " + current.Kind + " reminder"}})
		case patch.OffsetMinutes != nil:
			offset := int32(*patch.OffsetMinutes) //nolint:gosec // G115: 0 to 40320, checked by the contract
			updated.OffsetMinutes = &offset
		case patch.At != nil:
			updated.At = patch.At
		}
		task, err := q.GetTaskForUser(ctx, dbgen.GetTaskForUserParams{UserID: userID, ID: current.TaskID})
		if err != nil {
			return err
		}
		user, err := s.userSettings(ctx, q, userID)
		if err != nil {
			return err
		}
		updated.NextFireAt = reminder.NextFire(asComputation(updated), taskForComputation(task), user, s.now())
		if sameReminder(updated, current) {
			result = current
			return nil
		}
		if err := s.writeReminder(ctx, q, &updated); err != nil {
			return err
		}
		result = updated
		return nil
	})
	if err != nil {
		return Reminder{}, wrap("patch reminder", err)
	}
	return result, nil
}

// DeleteReminder soft-deletes a reminder; deleting is final for the user
// (D-19). Repeating it does nothing (D-22); while its task is deleted it
// answers 404, as checklist items do (D-45).
func (s *Service) DeleteReminder(ctx context.Context, userID, id uuid.UUID) error {
	err := s.inTx(ctx, func(q *dbgen.Queries) error {
		row, err := q.LockReminderForUser(ctx, dbgen.LockReminderForUserParams{UserID: userID, ID: id})
		if isNoRows(err) || (err == nil && row.TaskDeleted) {
			return ErrNotFound
		}
		if err != nil {
			return err
		}
		if row.Reminder.DeletedAt != nil {
			return nil
		}
		updated := row.Reminder
		now := s.now()
		updated.DeletedAt = &now
		updated.NextFireAt = nil
		return s.writeReminder(ctx, q, &updated)
	})
	return wrap("delete reminder", err)
}

// recompute brings next_fire_at of a task's live reminders in step with
// the task (SPEC section 6): after a change of its due fields, status or
// rule, a delete or a restore. With dropOneOff (a completion or an advance,
// R-8) absolute and snooze reminders are deleted instead. The caller holds
// the task's lock; reminders are locked after it.
func (s *Service) recompute(ctx context.Context, q *dbgen.Queries, userID uuid.UUID, task dbgen.Task, dropOneOff bool) error {
	rows, err := q.LockLiveRemindersOfTask(ctx, task.ID)
	if err != nil || len(rows) == 0 {
		return err
	}
	user, err := s.userSettings(ctx, q, userID)
	if err != nil {
		return err
	}
	for _, row := range rows {
		updated := row
		if dropOneOff && row.Kind != reminder.KindRelative {
			now := s.now()
			updated.DeletedAt = &now
			updated.NextFireAt = nil
		} else {
			updated.NextFireAt = reminder.NextFire(asComputation(row), taskForComputation(task), user, s.now())
		}
		if sameReminder(updated, row) {
			continue
		}
		if err := s.writeReminder(ctx, q, &updated); err != nil {
			return err
		}
	}
	return nil
}

// recomputeTasks is recompute for tasks known by id, after a write that
// touched many at once (deleting or restoring a list).
func (s *Service) recomputeTasks(ctx context.Context, q *dbgen.Queries, userID uuid.UUID, ids []uuid.UUID) error {
	for _, id := range ids {
		task, err := q.GetTaskForUser(ctx, dbgen.GetTaskForUserParams{UserID: userID, ID: id})
		if err != nil {
			return err
		}
		if err := s.recompute(ctx, q, userID, task, false); err != nil {
			return err
		}
	}
	return nil
}

// RecomputeUserReminders recomputes every live reminder of the user's tasks,
// after a change of the profile zone or default reminder time (SPEC section
// 6). It runs in the caller's transaction, q.
func (s *Service) RecomputeUserReminders(ctx context.Context, q *dbgen.Queries, userID uuid.UUID) error {
	rows, err := q.LockLiveRemindersOfUser(ctx, userID)
	if err != nil || len(rows) == 0 {
		return err
	}
	user, err := s.userSettings(ctx, q, userID)
	if err != nil {
		return err
	}
	for _, row := range rows {
		updated := row.Reminder
		updated.NextFireAt = reminder.NextFire(asComputation(row.Reminder), taskForComputation(row.Task), user, s.now())
		if sameReminder(updated, row.Reminder) {
			continue
		}
		if err := s.writeReminder(ctx, q, &updated); err != nil {
			return err
		}
	}
	return nil
}

// FireReminder records that the scheduler fired a reminder: nothing is
// pending any more, and a snooze reminder becomes a tombstone (D-32). The
// caller claimed the row with FOR UPDATE SKIP LOCKED.
func (s *Service) FireReminder(ctx context.Context, q *dbgen.Queries, row Reminder) error {
	updated := row
	now := s.now()
	updated.NextFireAt = nil
	updated.LastFiredAt = &now
	if row.Kind == reminder.KindSnooze {
		updated.DeletedAt = &now
	}
	return s.writeReminder(ctx, q, &updated)
}

// userSettings reads the zone and default time the computation needs. In
// V1 a list has one member, its owner, so the caller's settings are the
// owner's.
func (s *Service) userSettings(ctx context.Context, q *dbgen.Queries, userID uuid.UUID) (reminder.User, error) {
	user, err := q.GetUser(ctx, userID)
	if err != nil {
		return reminder.User{}, err
	}
	zone, err := time.LoadLocation(user.Timezone)
	if err != nil {
		return reminder.User{}, fmt.Errorf("user time zone: %w", err)
	}
	return reminder.User{Zone: zone, AllDayMinutes: localtime.Minutes(user.AllDayReminderTime)}, nil
}

// taskForComputation is the part of a task row the computation reads.
func taskForComputation(task dbgen.Task) reminder.Task {
	t := reminder.Task{
		Pending: task.Status == StatusOpen && task.DeletedAt == nil,
		DueDate: task.DueDate,
	}
	if task.DueTime.Valid {
		minutes := localtime.Minutes(task.DueTime)
		t.DueMinutes = &minutes
		if task.DueTz != nil {
			// Zones were validated when written.
			if zone, err := time.LoadLocation(*task.DueTz); err == nil {
				t.DueZone = zone
			}
		}
	}
	return t
}

func asComputation(row dbgen.Reminder) reminder.Reminder {
	r := reminder.Reminder{Kind: row.Kind}
	if row.OffsetMinutes != nil {
		r.OffsetMinutes = int(*row.OffsetMinutes)
	}
	if row.At != nil {
		r.At = *row.At
	}
	return r
}

// sameReminder compares what a write can change.
func sameReminder(a, b dbgen.Reminder) bool {
	return equalPointers(a.OffsetMinutes, b.OffsetMinutes) && equalTimes(a.At, b.At) &&
		equalTimes(a.NextFireAt, b.NextFireAt) && equalTimes(a.LastFiredAt, b.LastFiredAt) &&
		equalTimes(a.DeletedAt, b.DeletedAt)
}

func equalTimes(a, b *time.Time) bool {
	if a == nil || b == nil {
		return a == b
	}
	return a.Equal(*b)
}

// writeReminder stores a changed reminder: one more version and a new seq
// (D-07).
func (s *Service) writeReminder(ctx context.Context, q *dbgen.Queries, row *dbgen.Reminder) error {
	seq, err := q.NextSeq(ctx)
	if err != nil {
		return err
	}
	row.Version++
	row.Seq = seq
	row.UpdatedAt = s.now()
	return q.UpdateReminder(ctx, dbgen.UpdateReminderParams{
		ID: row.ID, OffsetMinutes: row.OffsetMinutes, At: row.At, NextFireAt: row.NextFireAt,
		LastFiredAt: row.LastFiredAt, Version: row.Version, Seq: row.Seq, UpdatedAt: row.UpdatedAt,
		DeletedAt: row.DeletedAt,
	})
}
