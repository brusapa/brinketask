package tasks

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"

	"github.com/brusapa/brinketask/internal/storage/dbgen"
)

// TaskQuery holds the filters of GET /tasks. Nil fields do not filter.
type TaskQuery struct {
	ListID  *uuid.UUID
	TagID   *uuid.UUID
	Status  *string
	DueFrom *time.Time
	DueTo   *time.Time
	// Search is matched as a substring of title or description, ignoring
	// case and accents (D-42).
	Search *string
	// SortByDue orders by due date instead of position (D-43).
	SortByDue bool
	// Trash lists the tasks deleted on their own that can still be
	// restored (D-40).
	Trash  bool
	Cursor *string
	Limit  int
}

// positionCursor and dueCursor carry the sort key of the last task of a
// page; the next page starts after it (keyset pagination). Unlike an
// offset, this stays correct while tasks are added or removed between
// pages.
type positionCursor struct {
	Kind         string    `json:"k"`
	ListPosition string    `json:"lp"`
	ListID       uuid.UUID `json:"l"`
	Position     string    `json:"p"`
	ID           uuid.UUID `json:"i"`
}

func (c positionCursor) cursorKind() string { return c.Kind }

type dueCursor struct {
	Kind     string    `json:"k"`
	DueDate  string    `json:"d"` // YYYY-MM-DD, or "infinity" for no date
	Timed    bool      `json:"t"`
	DueTime  string    `json:"h"` // HH:MM:SS.ffffff; "00:00:00" when not timed
	Position string    `json:"p"`
	ID       uuid.UUID `json:"i"`
}

func (c dueCursor) cursorKind() string { return c.Kind }

const (
	cursorTasksByPosition = "tasks-position"
	cursorTasksByDue      = "tasks-due"
)

// QueryTasks returns one page of the caller's tasks and the cursor of the
// next page, or nil on the last page.
func (s *Service) QueryTasks(ctx context.Context, userID uuid.UUID, query TaskQuery) ([]Task, *string, error) {
	q := dbgen.New(s.pool)
	if err := s.checkFilters(ctx, q, userID, query.ListID, query.TagID); err != nil {
		return nil, nil, err
	}

	status := query.Status
	if status == nil && !query.Trash {
		open := StatusOpen
		status = &open
	}
	var pattern *string
	if query.Search != nil && *query.Search != "" {
		p := "%" + escapeLike(*query.Search) + "%"
		pattern = &p
	}
	// One row more than asked tells whether another page follows.
	limit := int32(query.Limit) + 1 //nolint:gosec // G115: the contract caps limit at 500
	restorableSince := s.now().Add(-RestoreWindow)

	var rows []dbgen.Task
	// cursorOf builds the cursor that continues after a given task, for the
	// chosen order.
	var cursorOf func(dbgen.Task) string
	if query.SortByDue {
		params := dbgen.QueryTasksByDueParams{
			UserID: userID, Trash: query.Trash, RestorableSince: restorableSince,
			Status: status, ListID: query.ListID, TagID: query.TagID,
			DueFrom: query.DueFrom, DueTo: query.DueTo, Pattern: pattern, Lim: limit,
		}
		if query.Cursor != nil {
			var c dueCursor
			if err := decodeCursor(*query.Cursor, cursorTasksByDue, &c); err != nil {
				return nil, nil, err
			}
			params.HasAfter = true
			params.AfterDueDate, params.AfterTimed, params.AfterDueTime = c.DueDate, c.Timed, c.DueTime
			params.AfterPosition, params.AfterID = c.Position, c.ID
		}
		result, err := q.QueryTasksByDue(ctx, params)
		if err != nil {
			return nil, nil, fmt.Errorf("tasks: query tasks: %w", err)
		}
		for _, r := range result {
			rows = append(rows, r.Task)
		}
		cursorOf = func(t dbgen.Task) string { return encodeCursor(dueCursorOf(t)) }
	} else {
		params := dbgen.QueryTasksByPositionParams{
			UserID: userID, Trash: query.Trash, RestorableSince: restorableSince,
			Status: status, ListID: query.ListID, TagID: query.TagID,
			DueFrom: query.DueFrom, DueTo: query.DueTo, Pattern: pattern, Lim: limit,
		}
		if query.Cursor != nil {
			var c positionCursor
			if err := decodeCursor(*query.Cursor, cursorTasksByPosition, &c); err != nil {
				return nil, nil, err
			}
			params.HasAfter = true
			params.AfterListPosition, params.AfterListID = c.ListPosition, c.ListID
			params.AfterPosition, params.AfterID = c.Position, c.ID
		}
		result, err := q.QueryTasksByPosition(ctx, params)
		if err != nil {
			return nil, nil, fmt.Errorf("tasks: query tasks: %w", err)
		}
		listPositions := map[uuid.UUID]string{}
		for _, r := range result {
			rows = append(rows, r.Task)
			listPositions[r.Task.ListID] = r.ListPosition
		}
		cursorOf = func(t dbgen.Task) string {
			return encodeCursor(positionCursor{
				Kind: cursorTasksByPosition, ListPosition: listPositions[t.ListID],
				ListID: t.ListID, Position: t.Position, ID: t.ID,
			})
		}
	}

	var next *string
	if len(rows) > query.Limit {
		rows = rows[:query.Limit]
		c := cursorOf(rows[len(rows)-1])
		next = &c
	}
	tasks, err := withChecklists(ctx, q, rows)
	if err != nil {
		return nil, nil, fmt.Errorf("tasks: query tasks: %w", err)
	}
	return tasks, next, nil
}

// dueCursorOf builds the due-order sort key of a task, with the same NULL
// substitutes as the query.
func dueCursorOf(t dbgen.Task) dueCursor {
	c := dueCursor{Kind: cursorTasksByDue, DueDate: "infinity", DueTime: "00:00:00", Position: t.Position, ID: t.ID}
	if t.DueDate != nil {
		c.DueDate = t.DueDate.Format(time.DateOnly)
	}
	if t.DueTime.Valid {
		c.Timed = true
		c.DueTime = formatTimeOfDay(t.DueTime)
	}
	return c
}

// formatTimeOfDay renders a PostgreSQL time at full precision, so the
// cursor compares exactly like the stored value.
func formatTimeOfDay(t pgtype.Time) string {
	micros := t.Microseconds
	const perSecond = int64(time.Second / time.Microsecond)
	seconds := micros / perSecond
	return fmt.Sprintf("%02d:%02d:%02d.%06d", seconds/3600, seconds/60%60, seconds%60, micros%perSecond)
}

// checkFilters makes list and tag filters that name something the caller
// does not have, or that is deleted, a 404 (D-36).
func (s *Service) checkFilters(ctx context.Context, q *dbgen.Queries, userID uuid.UUID, listID, tagID *uuid.UUID) error {
	if listID != nil {
		row, err := q.GetListForUser(ctx, dbgen.GetListForUserParams{UserID: userID, ID: *listID})
		if isNoRows(err) || (err == nil && row.List.DeletedAt != nil) {
			return ErrNotFound
		}
		if err != nil {
			return fmt.Errorf("tasks: check list filter: %w", err)
		}
	}
	if tagID != nil {
		tag, err := q.GetTagForUser(ctx, dbgen.GetTagForUserParams{ID: *tagID, OwnerID: userID})
		if isNoRows(err) || (err == nil && tag.DeletedAt != nil) {
			return ErrNotFound
		}
		if err != nil {
			return fmt.Errorf("tasks: check tag filter: %w", err)
		}
	}
	return nil
}

// escapeLike makes the LIKE wildcards % and _, and the escape character
// itself, match literally (the queries use ESCAPE '\').
func escapeLike(s string) string {
	return strings.NewReplacer(`\`, `\\`, `%`, `\%`, `_`, `\_`).Replace(s)
}
