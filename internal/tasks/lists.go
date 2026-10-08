package tasks

import (
	"context"
	"fmt"

	"github.com/google/uuid"
	"github.com/oapi-codegen/nullable"

	"github.com/brusapa/brinketask/internal/storage/dbgen"
)

// NewList is a list to create. The client generates its id (D-03).
type NewList struct {
	ID       uuid.UUID
	Name     string
	Color    *string
	Position string
}

// ListPatch is a merge patch of a list (D-05): nil pointers and
// unspecified nullables leave their field alone.
type ListPatch struct {
	Name     *string
	Color    nullable.Nullable[string]
	Position *string
}

// Lists returns the caller's live lists, or with trash set, the deleted
// lists that can still be restored. Ordered by position, then id.
func (s *Service) Lists(ctx context.Context, userID uuid.UUID, trash bool) ([]List, error) {
	rows, err := dbgen.New(s.pool).ListListsForUser(ctx, dbgen.ListListsForUserParams{
		UserID:          userID,
		Trash:           trash,
		RestorableSince: s.clock.Now().Add(-RestoreWindow),
	})
	if err != nil {
		return nil, fmt.Errorf("tasks: list lists: %w", err)
	}
	lists := make([]List, len(rows))
	for i, row := range rows {
		lists[i] = List{List: row.List, Role: row.Role}
	}
	return lists, nil
}

// GetList returns one of the caller's live lists.
func (s *Service) GetList(ctx context.Context, userID, id uuid.UUID) (List, error) {
	row, err := dbgen.New(s.pool).GetListForUser(ctx, dbgen.GetListForUserParams{UserID: userID, ID: id})
	if isNoRows(err) || (err == nil && row.List.DeletedAt != nil) {
		return List{}, ErrNotFound
	}
	if err != nil {
		return List{}, fmt.Errorf("tasks: get list: %w", err)
	}
	return List{List: row.List, Role: row.Role}, nil
}

// CreateList creates a list owned by the caller, together with the owner's
// list_members row. created is false when the id already named one of the
// caller's lists, which is returned as it is, even if deleted (D-04, D-23).
// An id owned by someone else is a conflict.
func (s *Service) CreateList(ctx context.Context, userID uuid.UUID, in NewList) (List, bool, error) {
	return withRetry(func() (List, bool, error) {
		var list List
		created := false
		err := s.inTx(ctx, func(q *dbgen.Queries) error {
			existing, err := q.GetListForUser(ctx, dbgen.GetListForUserParams{UserID: userID, ID: in.ID})
			if err == nil {
				list = List{List: existing.List, Role: existing.Role}
				return nil
			}
			if !isNoRows(err) {
				return err
			}
			taken, err := q.ListExists(ctx, in.ID)
			if err != nil {
				return err
			}
			if taken {
				return conflict("the id belongs to another user's list")
			}

			now := s.clock.Now()
			seq, err := q.NextSeq(ctx)
			if err != nil {
				return err
			}
			err = q.InsertList(ctx, dbgen.InsertListParams{
				ID: in.ID, OwnerID: userID, Name: in.Name, Color: in.Color,
				Position: in.Position, IsInbox: false, Seq: seq, Now: now,
			})
			if err != nil {
				return err
			}
			err = q.InsertListMember(ctx, dbgen.InsertListMemberParams{ListID: in.ID, UserID: userID, Role: roleOwner})
			if err != nil {
				return err
			}
			row, err := q.GetListForUser(ctx, dbgen.GetListForUserParams{UserID: userID, ID: in.ID})
			if err != nil {
				return err
			}
			list = List{List: row.List, Role: row.Role}
			created = true
			return nil
		})
		if err != nil {
			return List{}, false, wrap("create list", err)
		}
		return list, created, nil
	})
}

// PatchList applies a merge patch to one of the caller's live lists. The
// inbox cannot be renamed. A patch that changes nothing writes nothing
// (D-41).
func (s *Service) PatchList(ctx context.Context, userID, id uuid.UUID, patch ListPatch) (List, error) {
	var list List
	err := s.inTx(ctx, func(q *dbgen.Queries) error {
		row, err := q.LockListForUser(ctx, dbgen.LockListForUserParams{UserID: userID, ID: id})
		if isNoRows(err) || (err == nil && row.List.DeletedAt != nil) {
			return ErrNotFound
		}
		if err != nil {
			return err
		}
		updated := row.List
		if patch.Name != nil {
			updated.Name = *patch.Name
		}
		if patch.Color.IsSpecified() {
			updated.Color = nullableToPointer(patch.Color)
		}
		if patch.Position != nil {
			updated.Position = *patch.Position
		}

		if updated.IsInbox && updated.Name != row.List.Name {
			return conflict("the inbox cannot be renamed")
		}
		if sameList(updated, row.List) {
			list = List{List: row.List, Role: row.Role}
			return nil
		}

		if err := s.writeList(ctx, q, &updated); err != nil {
			return err
		}
		list = List{List: updated, Role: row.Role}
		return nil
	})
	if err != nil {
		return List{}, wrap("patch list", err)
	}
	return list, nil
}

// DeleteList soft-deletes one of the caller's lists and, in the same
// transaction, its live tasks, marking them as deleted with the list
// (D-20). Deleting a deleted list does nothing (D-22). The inbox cannot be
// deleted.
func (s *Service) DeleteList(ctx context.Context, userID, id uuid.UUID) error {
	err := s.inTx(ctx, func(q *dbgen.Queries) error {
		row, err := q.LockListForUser(ctx, dbgen.LockListForUserParams{UserID: userID, ID: id})
		if isNoRows(err) {
			return ErrNotFound
		}
		if err != nil {
			return err
		}
		if row.List.DeletedAt != nil {
			return nil
		}
		if row.List.IsInbox {
			return conflict("the inbox cannot be deleted")
		}

		taskIDs, err := q.LockLiveTaskIDsInList(ctx, id)
		if err != nil {
			return err
		}
		now := s.clock.Now()
		updated := row.List
		updated.DeletedAt = &now
		if err := s.writeList(ctx, q, &updated); err != nil {
			return err
		}
		if len(taskIDs) == 0 {
			return nil
		}
		seqs, err := reserveSeqs(ctx, q, len(taskIDs))
		if err != nil {
			return err
		}
		return q.DeleteTasksWithList(ctx, dbgen.DeleteTasksWithListParams{
			Now: now, ListID: id, Ids: taskIDs, Seqs: seqs,
		})
	})
	return wrap("delete list", err)
}

// RestoreList undeletes one of the caller's lists within the restore
// window, together with the tasks deleted with it (D-20). Tasks deleted on
// their own before stay in the trash. Restoring a live list returns it as
// it is.
func (s *Service) RestoreList(ctx context.Context, userID, id uuid.UUID) (List, error) {
	var list List
	err := s.inTx(ctx, func(q *dbgen.Queries) error {
		row, err := q.LockListForUser(ctx, dbgen.LockListForUserParams{UserID: userID, ID: id})
		if isNoRows(err) {
			return ErrNotFound
		}
		if err != nil {
			return err
		}
		list = List{List: row.List, Role: row.Role}
		if row.List.DeletedAt == nil {
			return nil
		}
		now := s.clock.Now()
		if !restorable(*row.List.DeletedAt, now) {
			return conflict("the list was deleted more than 30 days ago")
		}

		taskIDs, err := q.LockTaskIDsDeletedWithList(ctx, &id)
		if err != nil {
			return err
		}
		updated := row.List
		updated.DeletedAt = nil
		if err := s.writeList(ctx, q, &updated); err != nil {
			return err
		}
		list.List = updated
		if len(taskIDs) == 0 {
			return nil
		}
		seqs, err := reserveSeqs(ctx, q, len(taskIDs))
		if err != nil {
			return err
		}
		return q.RestoreTasksWithList(ctx, dbgen.RestoreTasksWithListParams{Now: now, Ids: taskIDs, Seqs: seqs})
	})
	if err != nil {
		return List{}, wrap("restore list", err)
	}
	return list, nil
}

// writeList stores a changed list: one more version and a new seq (D-07).
// It updates the fields of list it sets.
func (s *Service) writeList(ctx context.Context, q *dbgen.Queries, list *dbgen.List) error {
	seq, err := q.NextSeq(ctx)
	if err != nil {
		return err
	}
	list.Version++
	list.Seq = seq
	list.UpdatedAt = s.clock.Now()
	return q.UpdateList(ctx, dbgen.UpdateListParams{
		ID: list.ID, Name: list.Name, Color: list.Color, Position: list.Position,
		Version: list.Version, Seq: list.Seq, UpdatedAt: list.UpdatedAt, DeletedAt: list.DeletedAt,
	})
}

// sameList compares the fields a patch can change.
func sameList(a, b dbgen.List) bool {
	return a.Name == b.Name && equalPointers(a.Color, b.Color) && a.Position == b.Position
}
