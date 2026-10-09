package tasks

import (
	"github.com/brusapa/brinketask/internal/storage/dbgen"
)

// The resources are the database rows, plus what the API shows with them.
// Embedding (a struct field without a name) makes the row's fields
// accessible directly, e.g. list.Name.

// List is a list with the caller's role in it.
type List struct {
	dbgen.List
	Role string
}

// Task is a task with its live checklist items, ordered by position.
type Task struct {
	dbgen.Task
	ChecklistItems []dbgen.ChecklistItem
}

// Tag, ChecklistItem and Completion are the rows as they are. "=" makes
// them aliases: the same type under a second name.
type (
	Tag           = dbgen.Tag
	ChecklistItem = dbgen.ChecklistItem
	Completion    = dbgen.TaskCompletion
)

// Task status values (SPEC section 4).
const (
	StatusOpen    = "open"
	StatusDone    = "done"
	StatusDropped = "dropped"
)

// repeat_from values (SPEC section 4, R-3).
const (
	RepeatFromDue        = "due"
	RepeatFromCompletion = "completion"
)

// roleOwner is the only role V1 creates.
const roleOwner = "owner"
