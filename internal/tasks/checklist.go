package tasks

import (
	"context"

	"github.com/google/uuid"

	"github.com/brusapa/brinketask/internal/storage/dbgen"
)

// ChecklistItemPatch is a merge patch of a checklist item (D-05).
type ChecklistItemPatch struct {
	Title    *string
	IsDone   *bool
	Position *string
}

// CreateChecklistItem adds an item to one of the caller's live tasks.
// created is false when the id already named one of the caller's items,
// which is returned as it is (D-04, D-23); someone else's id is a conflict.
func (s *Service) CreateChecklistItem(ctx context.Context, userID, taskID uuid.UUID, in NewChecklistItem) (ChecklistItem, bool, error) {
	return withRetry(func() (ChecklistItem, bool, error) {
		var item ChecklistItem
		created := false
		err := s.inTx(ctx, func(q *dbgen.Queries) error {
			task, err := q.LockTaskForUser(ctx, dbgen.LockTaskForUserParams{UserID: userID, ID: taskID})
			if isNoRows(err) || (err == nil && task.DeletedAt != nil) {
				return ErrNotFound
			}
			if err != nil {
				return err
			}

			existing, err := q.GetChecklistItemForUser(ctx, dbgen.GetChecklistItemForUserParams{UserID: userID, ID: in.ID})
			if err == nil {
				item = existing.ChecklistItem
				return nil
			}
			if !isNoRows(err) {
				return err
			}
			taken, err := q.ChecklistItemExists(ctx, in.ID)
			if err != nil {
				return err
			}
			if taken {
				return conflict("the id belongs to another user's checklist item")
			}

			now := s.now()
			seq, err := q.NextSeq(ctx)
			if err != nil {
				return err
			}
			err = q.InsertChecklistItem(ctx, dbgen.InsertChecklistItemParams{
				ID: in.ID, TaskID: taskID, Title: in.Title, IsDone: in.IsDone,
				Position: in.Position, Seq: seq, Now: now,
			})
			if err != nil {
				return err
			}
			item = ChecklistItem{
				ID: in.ID, TaskID: taskID, Title: in.Title, IsDone: in.IsDone, Position: in.Position,
				Version: 1, Seq: seq, CreatedAt: now, UpdatedAt: now,
			}
			created = true
			return nil
		})
		if err != nil {
			return ChecklistItem{}, false, wrap("create checklist item", err)
		}
		return item, created, nil
	})
}

// PatchChecklistItem applies a merge patch to a live item of one of the
// caller's live tasks (D-45).
func (s *Service) PatchChecklistItem(ctx context.Context, userID, id uuid.UUID, patch ChecklistItemPatch) (ChecklistItem, error) {
	var item ChecklistItem
	err := s.inTx(ctx, func(q *dbgen.Queries) error {
		row, err := q.LockChecklistItemForUser(ctx, dbgen.LockChecklistItemForUserParams{UserID: userID, ID: id})
		if isNoRows(err) || (err == nil && (row.ChecklistItem.DeletedAt != nil || row.TaskDeleted)) {
			return ErrNotFound
		}
		if err != nil {
			return err
		}
		current := row.ChecklistItem
		updated := current
		if patch.Title != nil {
			updated.Title = *patch.Title
		}
		if patch.IsDone != nil {
			updated.IsDone = *patch.IsDone
		}
		if patch.Position != nil {
			updated.Position = *patch.Position
		}
		if updated.Title == current.Title && updated.IsDone == current.IsDone && updated.Position == current.Position {
			item = current
			return nil
		}
		if err := s.writeChecklistItem(ctx, q, &updated); err != nil {
			return err
		}
		item = updated
		return nil
	})
	if err != nil {
		return ChecklistItem{}, wrap("patch checklist item", err)
	}
	return item, nil
}

// DeleteChecklistItem soft-deletes an item. Deleting is final for the user
// (D-19) but leaves a tombstone for sync. Repeating it does nothing (D-22).
func (s *Service) DeleteChecklistItem(ctx context.Context, userID, id uuid.UUID) error {
	err := s.inTx(ctx, func(q *dbgen.Queries) error {
		row, err := q.LockChecklistItemForUser(ctx, dbgen.LockChecklistItemForUserParams{UserID: userID, ID: id})
		if isNoRows(err) || (err == nil && row.TaskDeleted) {
			return ErrNotFound
		}
		if err != nil {
			return err
		}
		if row.ChecklistItem.DeletedAt != nil {
			return nil
		}
		item := row.ChecklistItem
		now := s.now()
		item.DeletedAt = &now
		return s.writeChecklistItem(ctx, q, &item)
	})
	return wrap("delete checklist item", err)
}

// writeChecklistItem stores a changed item: one more version and a new seq
// (D-07).
func (s *Service) writeChecklistItem(ctx context.Context, q *dbgen.Queries, item *dbgen.ChecklistItem) error {
	seq, err := q.NextSeq(ctx)
	if err != nil {
		return err
	}
	item.Version++
	item.Seq = seq
	item.UpdatedAt = s.now()
	return q.UpdateChecklistItem(ctx, dbgen.UpdateChecklistItemParams{
		ID: item.ID, Title: item.Title, IsDone: item.IsDone, Position: item.Position,
		Version: item.Version, Seq: item.Seq, UpdatedAt: item.UpdatedAt, DeletedAt: item.DeletedAt,
	})
}
