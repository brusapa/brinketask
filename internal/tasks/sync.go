package tasks

import (
	"context"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/brusapa/brinketask/internal/storage/dbgen"
)

// ChangesPage is one page of /sync/changes (SPEC section 8).
type ChangesPage struct {
	Lists          []List
	Tasks          []dbgen.Task
	ChecklistItems []ChecklistItem
	Tags           []Tag
	// NextCursor resumes after this page; it is returned also when HasMore
	// is false, so the client can ask for later changes.
	NextCursor string
	HasMore    bool
}

type syncCursor struct {
	Kind string `json:"k"`
	Seq  int64  `json:"s"`
}

func (c syncCursor) cursorKind() string { return c.Kind }

const cursorSync = "sync"

// Changes returns the caller's resources whose seq is greater than the
// cursor's, tombstones included, oldest change first. Without a cursor it
// returns the full live state: no tombstones, and no items of deleted
// tasks.
//
// A page holds up to limit rows across all resource types. The cursor is
// the global seq up to which the client is complete. That works because
// writers take seqs one at a time under a lock held until commit (D-07):
// once seq N is visible, every seq below N is visible too, so "seq >
// cursor" never skips a change.
//
// A cursor older than the purge watermark answers ErrCursorExpired (D-21):
// tombstones the client needs may be gone.
func (s *Service) Changes(ctx context.Context, userID uuid.UUID, cursor *string, limit int) (ChangesPage, error) {
	var after int64
	liveOnly := cursor == nil
	if cursor != nil {
		var c syncCursor
		if err := decodeCursor(*cursor, cursorSync, &c); err != nil {
			return ChangesPage{}, err
		}
		after = c.Seq
	}

	var page ChangesPage
	// One read-only REPEATABLE READ transaction: every query sees the same
	// snapshot, so the page boundary and the rows agree.
	options := pgx.TxOptions{IsoLevel: pgx.RepeatableRead, AccessMode: pgx.ReadOnly}
	err := pgx.BeginTxFunc(ctx, s.pool, options, func(tx pgx.Tx) error {
		q := dbgen.New(tx)
		state, err := q.GetSyncState(ctx)
		if err != nil {
			return err
		}
		if cursor != nil && after < state.PurgedUpToSeq {
			return ErrCursorExpired
		}

		// limit+1 seqs tell whether another page follows; the page ends at
		// the limit-th one. Without more, it ends at the newest seq of the
		// snapshot, which is where the next call starts.
		seqs, err := q.SyncPageSeqs(ctx, dbgen.SyncPageSeqsParams{
			UserID: userID, After: after, LiveOnly: liveOnly, Lim: int32(limit) + 1, //nolint:gosec // G115: limit <= 500
		})
		if err != nil {
			return err
		}
		upper := max(state.Seq, after)
		if len(seqs) > limit {
			page.HasMore = true
			upper = seqs[limit-1]
		}

		lower, upperBound := after, upper
		lists, err := q.SyncLists(ctx, dbgen.SyncListsParams{UserID: userID, After: lower, Upper: upperBound, LiveOnly: liveOnly})
		if err != nil {
			return err
		}
		page.Lists = make([]List, len(lists))
		for i, row := range lists {
			page.Lists[i] = List{List: row.List, Role: row.Role}
		}
		if page.Tasks, err = q.SyncTasks(ctx, dbgen.SyncTasksParams{UserID: userID, After: lower, Upper: upperBound, LiveOnly: liveOnly}); err != nil {
			return err
		}
		if page.ChecklistItems, err = q.SyncChecklistItems(ctx, dbgen.SyncChecklistItemsParams{UserID: userID, After: lower, Upper: upperBound, LiveOnly: liveOnly}); err != nil {
			return err
		}
		if page.Tags, err = q.SyncTags(ctx, dbgen.SyncTagsParams{UserID: userID, After: lower, Upper: upperBound, LiveOnly: liveOnly}); err != nil {
			return err
		}
		page.NextCursor = encodeCursor(syncCursor{Kind: cursorSync, Seq: upper})
		return nil
	})
	if err != nil {
		return ChangesPage{}, wrap("sync changes", err)
	}
	return page, nil
}
