package httpapi

import (
	"time"

	"github.com/jackc/pgx/v5/pgtype"
	"github.com/oapi-codegen/nullable"
	openapi_types "github.com/oapi-codegen/runtime/types"

	"github.com/brusapa/brinketask/internal/localtime"

	"github.com/brusapa/brinketask/internal/tasks"
)

// Conversions between the rows of package tasks and the generated API
// types. The generated types use pointers for optional fields and
// nullable.Nullable for fields that may be null.

func listToAPI(l tasks.List) List {
	version := int(l.Version)
	isInbox := l.IsInbox
	role := ListRole(l.Role)
	return List{
		Id:        l.ID,
		Name:      l.Name,
		Color:     pointerToNullable(l.Color),
		Position:  l.Position,
		IsInbox:   &isInbox,
		Role:      &role,
		Version:   &version,
		CreatedAt: timePointer(l.CreatedAt),
		UpdatedAt: timePointer(l.UpdatedAt),
		DeletedAt: utcNullable(l.DeletedAt),
	}
}

// pointerToNullable maps nil to JSON null and anything else to its value.
func pointerToNullable[T any](p *T) nullable.Nullable[T] {
	if p == nil {
		return nullable.NewNullNullable[T]()
	}
	return nullable.NewNullableWithValue(*p)
}

// timePointer returns a pointer to a copy of t, in UTC as the API requires
// (SPEC section 8).
func timePointer(t time.Time) *time.Time {
	utc := t.UTC()
	return &utc
}

// nullableToPointer converts an optional, nullable request field where
// absent and null mean the same (no value): nil, or a pointer to the value.
func nullableToPointer[T any](n nullable.Nullable[T]) *T {
	if !n.IsSpecified() || n.IsNull() {
		return nil
	}
	value := n.MustGet()
	return &value
}

func tagToAPI(t tasks.Tag) Tag {
	version := int(t.Version)
	return Tag{
		Id:        t.ID,
		Name:      t.Name,
		Color:     pointerToNullable(t.Color),
		Version:   &version,
		CreatedAt: timePointer(t.CreatedAt),
		UpdatedAt: timePointer(t.UpdatedAt),
		DeletedAt: utcNullable(t.DeletedAt),
	}
}

// taskToAPI converts a task. Tasks in /sync/changes come without their
// checklist and reminders (withChildren false); everywhere else they carry
// both, and reminders are always empty until phase 5 (D-34).
func taskToAPI(t tasks.Task, withChildren bool) Task {
	version := int(t.Version)
	doneCount := int(t.RecurrenceDoneCount)
	task := Task{
		Id:                  t.ID,
		ListId:              t.ListID,
		Title:               t.Title,
		Description:         t.Description,
		Status:              TaskStatus(t.Status),
		Priority:            int(t.Priority),
		Position:            t.Position,
		DueDate:             dateToAPI(t.DueDate),
		DueTime:             timeOfDayToAPI(t.DueTime),
		DueTz:               pointerToNullable(t.DueTz),
		Rrule:               pointerToNullable(t.Rrule),
		RepeatFrom:          RepeatFrom(t.RepeatFrom),
		RecurrenceDoneCount: &doneCount,
		CompletedAt:         utcNullable(t.CompletedAt),
		TagIds:              t.TagIds,
		Version:             &version,
		CreatedAt:           timePointer(t.CreatedAt),
		UpdatedAt:           timePointer(t.UpdatedAt),
		DeletedAt:           utcNullable(t.DeletedAt),
	}
	if task.TagIds == nil {
		task.TagIds = []openapi_types.UUID{}
	}
	if withChildren {
		items := make([]ChecklistItem, len(t.ChecklistItems))
		for i, item := range t.ChecklistItems {
			items[i] = checklistItemToAPI(item)
		}
		reminders := make([]Reminder, len(t.Reminders))
		for i, r := range t.Reminders {
			reminders[i] = reminderToAPI(r)
		}
		task.ChecklistItems = &items
		task.Reminders = &reminders
	}
	return task
}

func checklistItemToAPI(c tasks.ChecklistItem) ChecklistItem {
	version := int(c.Version)
	taskID := c.TaskID
	return ChecklistItem{
		Id:        c.ID,
		TaskId:    &taskID,
		Title:     c.Title,
		IsDone:    c.IsDone,
		Position:  c.Position,
		Version:   &version,
		CreatedAt: timePointer(c.CreatedAt),
		UpdatedAt: timePointer(c.UpdatedAt),
		DeletedAt: utcNullable(c.DeletedAt),
	}
}

// dateToAPI converts a calendar date stored as UTC midnight.
func dateToAPI(t *time.Time) nullable.Nullable[openapi_types.Date] {
	if t == nil {
		return nullable.NewNullNullable[openapi_types.Date]()
	}
	return nullable.NewNullableWithValue(openapi_types.Date{Time: *t})
}

// timeOfDayToAPI converts PostgreSQL's time to "HH:MM", or null.
func timeOfDayToAPI(t pgtype.Time) nullable.Nullable[LocalTime] {
	if !t.Valid {
		return nullable.NewNullNullable[LocalTime]()
	}
	return nullable.NewNullableWithValue(localtime.Format(t))
}

// utcNullable converts an optional timestamp, in UTC.
func utcNullable(t *time.Time) nullable.Nullable[time.Time] {
	if t == nil {
		return nullable.NewNullNullable[time.Time]()
	}
	return nullable.NewNullableWithValue(t.UTC())
}

// dateFromAPI is the inverse of dateToAPI for request fields.
func dateFromAPI(d *openapi_types.Date) *time.Time {
	if d == nil {
		return nil
	}
	t := time.Date(d.Year(), d.Month(), d.Day(), 0, 0, 0, 0, time.UTC)
	return &t
}

// nullableDateFromAPI converts a merge-patch date, keeping "absent" and
// "null" apart.
func nullableDateFromAPI(n nullable.Nullable[openapi_types.Date]) nullable.Nullable[time.Time] {
	switch {
	case !n.IsSpecified():
		return nullable.Nullable[time.Time]{}
	case n.IsNull():
		return nullable.NewNullNullable[time.Time]()
	default:
		d := n.MustGet()
		return nullable.NewNullableWithValue(*dateFromAPI(&d))
	}
}

func completionToAPI(c tasks.Completion) Completion {
	return Completion{
		Id:                c.ID,
		TaskId:            c.TaskID,
		Kind:              CompletionKind(c.Kind),
		OccurrenceDueDate: dateToAPI(c.OccurrenceDueDate),
		CompletedAt:       c.CompletedAt.UTC(),
	}
}

func completionResultToAPI(r tasks.CompletionResult) CompletionResult {
	result := CompletionResult{
		Applied:    r.Applied,
		Task:       taskToAPI(r.Task, true),
		Completion: nullable.NewNullNullable[Completion](),
	}
	if r.Completion != nil {
		result.Completion = nullable.NewNullableWithValue(completionToAPI(*r.Completion))
	}
	return result
}

func completionEntryToAPI(e tasks.CompletionEntry) CompletionEntry {
	return CompletionEntry{
		Id:                e.ID,
		TaskId:            e.TaskID,
		Kind:              CompletionEntryKind(e.Kind),
		OccurrenceDueDate: dateToAPI(e.OccurrenceDueDate),
		CompletedAt:       e.CompletedAt.UTC(),
		CanUndo:           e.CanUndo,
		Task: TaskSummary{
			Id:       e.TaskID,
			ListId:   e.ListID,
			Title:    e.Title,
			Priority: int(e.Priority),
			Rrule:    pointerToNullable(e.Rrule),
		},
	}
}

func reminderToAPI(r tasks.Reminder) Reminder {
	version := int(r.Version)
	taskID := r.TaskID
	reminder := Reminder{
		Id:         r.ID,
		TaskId:     &taskID,
		Kind:       ReminderKind(r.Kind),
		At:         utcNullable(r.At),
		NextFireAt: utcNullable(r.NextFireAt),
		Version:    &version,
		CreatedAt:  timePointer(r.CreatedAt),
		UpdatedAt:  timePointer(r.UpdatedAt),
		DeletedAt:  utcNullable(r.DeletedAt),
	}
	reminder.OffsetMinutes = nullable.NewNullNullable[int]()
	if r.OffsetMinutes != nil {
		reminder.OffsetMinutes = nullable.NewNullableWithValue(int(*r.OffsetMinutes))
	}
	return reminder
}

// newReminderFromAPI reads a reminder of a create body.
func newReminderFromAPI(r ReminderCreate) tasks.NewReminder {
	return tasks.NewReminder{
		ID:            r.Id,
		Kind:          string(r.Kind),
		OffsetMinutes: nullableToPointer(r.OffsetMinutes),
		At:            nullableToPointer(r.At),
	}
}
