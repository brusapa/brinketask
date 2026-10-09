-- name: NextSeq :one
-- Takes the next value of the global change counter (D-07). The UPDATE
-- locks the single sync_state row until the transaction ends, so concurrent
-- writers take numbers one at a time and a reader never sees seq N+1
-- committed before seq N.
UPDATE sync_state SET seq = seq + 1 RETURNING seq;

-- name: ReserveSeqs :one
-- Takes @n consecutive values of the counter at once and returns the last
-- one; the caller uses last-n+1 .. last. Same lock as NextSeq. A write that
-- touches many rows (deleting a list deletes its tasks) gives each row its
-- own seq this way with one statement.
UPDATE sync_state SET seq = seq + @n::bigint RETURNING seq;

-- name: GetSyncState :one
SELECT seq, purged_up_to_seq FROM sync_state;

-- The /sync/changes queries (SPEC section 8). A page holds the rows with
-- after < seq <= upper; SyncPageSeqs finds upper. With live_only (no
-- cursor: the full live state) tombstones are left out, and so are the
-- items of deleted tasks.

-- name: SyncPageSeqs :many
SELECT s.seq FROM (
    SELECT l.seq FROM lists l
    JOIN list_members m ON m.list_id = l.id AND m.user_id = @user_id
    WHERE l.seq > @after AND (NOT @live_only::boolean OR l.deleted_at IS NULL)
    UNION ALL
    SELECT t.seq FROM tasks t
    JOIN list_members m ON m.list_id = t.list_id AND m.user_id = @user_id
    WHERE t.seq > @after AND (NOT @live_only::boolean OR t.deleted_at IS NULL)
    UNION ALL
    SELECT c.seq FROM checklist_items c
    JOIN tasks t ON t.id = c.task_id
    JOIN list_members m ON m.list_id = t.list_id AND m.user_id = @user_id
    WHERE c.seq > @after
      AND (NOT @live_only::boolean OR (c.deleted_at IS NULL AND t.deleted_at IS NULL))
    UNION ALL
    SELECT r.seq FROM reminders r
    JOIN tasks t ON t.id = r.task_id
    JOIN list_members m ON m.list_id = t.list_id AND m.user_id = @user_id
    WHERE r.seq > @after
      AND (NOT @live_only::boolean OR (r.deleted_at IS NULL AND t.deleted_at IS NULL))
    UNION ALL
    SELECT g.seq FROM tags g
    WHERE g.owner_id = @user_id AND g.seq > @after
      AND (NOT @live_only::boolean OR g.deleted_at IS NULL)
) s
ORDER BY s.seq
LIMIT @lim;

-- name: SyncLists :many
SELECT sqlc.embed(l), m.role FROM lists l
JOIN list_members m ON m.list_id = l.id AND m.user_id = @user_id
WHERE l.seq > @after AND l.seq <= @upper AND (NOT @live_only::boolean OR l.deleted_at IS NULL)
ORDER BY l.seq;

-- name: SyncTasks :many
SELECT t.* FROM tasks t
JOIN list_members m ON m.list_id = t.list_id AND m.user_id = @user_id
WHERE t.seq > @after AND t.seq <= @upper AND (NOT @live_only::boolean OR t.deleted_at IS NULL)
ORDER BY t.seq;

-- name: SyncChecklistItems :many
SELECT c.* FROM checklist_items c
JOIN tasks t ON t.id = c.task_id
JOIN list_members m ON m.list_id = t.list_id AND m.user_id = @user_id
WHERE c.seq > @after AND c.seq <= @upper
  AND (NOT @live_only::boolean OR (c.deleted_at IS NULL AND t.deleted_at IS NULL))
ORDER BY c.seq;

-- name: SyncReminders :many
SELECT r.* FROM reminders r
JOIN tasks t ON t.id = r.task_id
JOIN list_members m ON m.list_id = t.list_id AND m.user_id = @user_id
WHERE r.seq > @after AND r.seq <= @upper
  AND (NOT @live_only::boolean OR (r.deleted_at IS NULL AND t.deleted_at IS NULL))
ORDER BY r.seq;

-- name: SyncTags :many
SELECT * FROM tags
WHERE owner_id = @user_id AND seq > @after AND seq <= @upper
  AND (NOT @live_only::boolean OR deleted_at IS NULL)
ORDER BY seq;
