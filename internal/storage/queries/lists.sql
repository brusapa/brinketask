-- name: InsertList :exec
-- A new list at version 1. The caller takes seq from NextSeq in the same
-- transaction (D-07) and adds the owner's list_members row.
INSERT INTO lists (id, owner_id, name, color, position, is_inbox, version, seq, created_at, updated_at)
VALUES (@id, @owner_id, @name, @color, @position, @is_inbox, 1, @seq, @now, @now);

-- name: InsertListMember :exec
INSERT INTO list_members (list_id, user_id, role) VALUES (@list_id, @user_id, @role);

-- name: GetListForUser :one
-- A list the user is a member of, live or deleted, with the user's role.
-- Every list access goes through list_members (CLAUDE.md).
SELECT sqlc.embed(l), m.role
FROM lists l
JOIN list_members m ON m.list_id = l.id AND m.user_id = @user_id
WHERE l.id = @id;

-- name: LockListForUser :one
-- Same as GetListForUser, locking the list until the transaction ends.
SELECT sqlc.embed(l), m.role
FROM lists l
JOIN list_members m ON m.list_id = l.id AND m.user_id = @user_id
WHERE l.id = @id
FOR UPDATE OF l;

-- name: LockLiveListForShare :one
-- The target list of a task being created or moved: it must be live and the
-- user's. FOR SHARE keeps it from being deleted until the task is written,
-- so a task never lands in a list deleted concurrently.
SELECT l.id
FROM lists l
JOIN list_members m ON m.list_id = l.id AND m.user_id = @user_id
WHERE l.id = @id AND l.deleted_at IS NULL
FOR SHARE OF l;

-- name: ListExists :one
SELECT EXISTS (SELECT 1 FROM lists WHERE id = @id);

-- name: ListListsForUser :many
-- Live lists, or with trash set, the lists deleted after restorable_since.
SELECT sqlc.embed(l), m.role
FROM lists l
JOIN list_members m ON m.list_id = l.id AND m.user_id = @user_id
WHERE CASE WHEN @trash::boolean
           THEN l.deleted_at > @restorable_since::timestamptz
           ELSE l.deleted_at IS NULL END
ORDER BY l.position, l.id;

-- name: UpdateList :exec
UPDATE lists
SET name = @name, color = @color, position = @position, version = @version, seq = @seq,
    updated_at = @updated_at, deleted_at = @deleted_at
WHERE id = @id;

-- name: LockLiveTaskIDsInList :many
SELECT id FROM tasks WHERE list_id = @list_id AND deleted_at IS NULL ORDER BY id FOR UPDATE;

-- name: DeleteTasksWithList :exec
-- Soft-deletes the given tasks as part of deleting their list (D-20). ids
-- and seqs are parallel arrays: each task gets its own seq.
UPDATE tasks t
SET deleted_at = @now::timestamptz, deleted_with_list_id = @list_id::uuid, version = t.version + 1,
    seq = v.seq, updated_at = @now
-- Two unnest calls in one SELECT list advance together, pairing ids[i]
-- with seqs[i].
FROM (SELECT unnest(@ids::uuid[]) AS id, unnest(@seqs::bigint[]) AS seq) AS v
WHERE t.id = v.id;

-- name: LockTaskIDsDeletedWithList :many
SELECT id FROM tasks WHERE deleted_with_list_id = @list_id ORDER BY id FOR UPDATE;

-- name: RestoreTasksWithList :exec
UPDATE tasks t
SET deleted_at = NULL, deleted_with_list_id = NULL, version = t.version + 1,
    seq = v.seq, updated_at = @now
-- Two unnest calls in one SELECT list advance together, pairing ids[i]
-- with seqs[i].
FROM (SELECT unnest(@ids::uuid[]) AS id, unnest(@seqs::bigint[]) AS seq) AS v
WHERE t.id = v.id;
