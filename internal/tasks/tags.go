package tasks

import (
	"context"
	"fmt"

	"github.com/google/uuid"
	"github.com/oapi-codegen/nullable"

	"github.com/brusapa/brinketask/internal/storage/dbgen"
)

// Tags belong to a user, not to a list; they are filtered by owner_id.

// NewTag is a tag to create. The client generates its id (D-03).
type NewTag struct {
	ID    uuid.UUID
	Name  string
	Color *string
}

// TagPatch is a merge patch of a tag (D-05).
type TagPatch struct {
	Name  *string
	Color nullable.Nullable[string]
}

// errNameTaken is the conflict of a duplicate live tag name.
var errNameTaken = &ConflictError{Reason: "another tag already has this name"}

// Tags returns the caller's live tags, ordered by name ignoring case.
func (s *Service) Tags(ctx context.Context, userID uuid.UUID) ([]Tag, error) {
	tags, err := dbgen.New(s.pool).ListLiveTags(ctx, userID)
	if err != nil {
		return nil, fmt.Errorf("tasks: list tags: %w", err)
	}
	return tags, nil
}

// CreateTag creates a tag of the caller. created is false when the id
// already named one of the caller's tags, which is returned as it is (D-04,
// D-23). Someone else's id, or the name of another live tag of the caller
// (ignoring case), is a conflict.
func (s *Service) CreateTag(ctx context.Context, userID uuid.UUID, in NewTag) (Tag, bool, error) {
	return withRetry(func() (Tag, bool, error) {
		var tag Tag
		created := false
		err := s.inTx(ctx, func(q *dbgen.Queries) error {
			existing, err := q.GetTagForUser(ctx, dbgen.GetTagForUserParams{ID: in.ID, OwnerID: userID})
			if err == nil {
				tag = existing
				return nil
			}
			if !isNoRows(err) {
				return err
			}
			taken, err := q.TagExists(ctx, in.ID)
			if err != nil {
				return err
			}
			if taken {
				return conflict("the id belongs to another user's tag")
			}
			if err := checkTagName(ctx, q, userID, in.ID, in.Name); err != nil {
				return err
			}

			now := s.clock.Now()
			seq, err := q.NextSeq(ctx)
			if err != nil {
				return err
			}
			err = q.InsertTag(ctx, dbgen.InsertTagParams{
				ID: in.ID, OwnerID: userID, Name: in.Name, Color: in.Color, Seq: seq, Now: now,
			})
			if err != nil {
				return err
			}
			tag = Tag{
				ID: in.ID, OwnerID: userID, Name: in.Name, Color: in.Color,
				Version: 1, Seq: seq, CreatedAt: now, UpdatedAt: now,
			}
			created = true
			return nil
		})
		if err != nil {
			return Tag{}, false, wrap("create tag", err)
		}
		return tag, created, nil
	})
}

// PatchTag applies a merge patch to one of the caller's live tags.
func (s *Service) PatchTag(ctx context.Context, userID, id uuid.UUID, patch TagPatch) (Tag, error) {
	var tag Tag
	err := s.inTx(ctx, func(q *dbgen.Queries) error {
		current, err := q.LockTagForUser(ctx, dbgen.LockTagForUserParams{ID: id, OwnerID: userID})
		if isNoRows(err) || (err == nil && current.DeletedAt != nil) {
			return ErrNotFound
		}
		if err != nil {
			return err
		}
		updated := current
		if patch.Name != nil {
			updated.Name = *patch.Name
		}
		if patch.Color.IsSpecified() {
			updated.Color = nullableToPointer(patch.Color)
		}
		if updated.Name == current.Name && equalPointers(updated.Color, current.Color) {
			tag = current
			return nil
		}
		if err := checkTagName(ctx, q, userID, id, updated.Name); err != nil {
			return err
		}
		if err := s.writeTag(ctx, q, &updated); err != nil {
			return err
		}
		tag = updated
		return nil
	})
	if err != nil {
		return Tag{}, wrap("patch tag", err)
	}
	return tag, nil
}

// DeleteTag soft-deletes one of the caller's tags and removes it from the
// tag_ids of every task of the caller, live or deleted, so a restored task
// does not bring it back. Each of those tasks is a write: new version and
// seq (D-07). Deleting a deleted tag does nothing (D-22).
func (s *Service) DeleteTag(ctx context.Context, userID, id uuid.UUID) error {
	err := s.inTx(ctx, func(q *dbgen.Queries) error {
		current, err := q.LockTagForUser(ctx, dbgen.LockTagForUserParams{ID: id, OwnerID: userID})
		if isNoRows(err) {
			return ErrNotFound
		}
		if err != nil {
			return err
		}
		if current.DeletedAt != nil {
			return nil
		}

		// The tag is locked first and the tasks second; writes of tasks
		// lock tags before tasks too, so the two cannot deadlock.
		taskIDs, err := q.LockTaskIDsWithTag(ctx, dbgen.LockTaskIDsWithTagParams{UserID: userID, TagID: id})
		if err != nil {
			return err
		}
		now := s.clock.Now()
		updated := current
		updated.DeletedAt = &now
		if err := s.writeTag(ctx, q, &updated); err != nil {
			return err
		}
		if len(taskIDs) == 0 {
			return nil
		}
		seqs, err := reserveSeqs(ctx, q, len(taskIDs))
		if err != nil {
			return err
		}
		return q.RemoveTagFromTasks(ctx, dbgen.RemoveTagFromTasksParams{TagID: id, Now: now, Ids: taskIDs, Seqs: seqs})
	})
	return wrap("delete tag", err)
}

// checkTagName fails when another live tag of the user has the name,
// ignoring case. The unique index tags_live_name enforces the same rule if
// two requests race; the caller turns that violation into the same
// conflict.
func checkTagName(ctx context.Context, q *dbgen.Queries, userID, id uuid.UUID, name string) error {
	taken, err := q.LiveTagNameTaken(ctx, dbgen.LiveTagNameTakenParams{OwnerID: userID, Name: name, ID: id})
	if err != nil {
		return err
	}
	if taken {
		return errNameTaken
	}
	return nil
}

// writeTag stores a changed tag: one more version and a new seq (D-07).
func (s *Service) writeTag(ctx context.Context, q *dbgen.Queries, tag *dbgen.Tag) error {
	seq, err := q.NextSeq(ctx)
	if err != nil {
		return err
	}
	tag.Version++
	tag.Seq = seq
	tag.UpdatedAt = s.clock.Now()
	err = q.UpdateTag(ctx, dbgen.UpdateTagParams{
		ID: tag.ID, Name: tag.Name, Color: tag.Color, Version: tag.Version, Seq: tag.Seq,
		UpdatedAt: tag.UpdatedAt, DeletedAt: tag.DeletedAt,
	})
	if constraintOf(err) == "tags_live_name" {
		return errNameTaken
	}
	return err
}
