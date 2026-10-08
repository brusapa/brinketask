-- Tags belong to a user (owner_id), not to a list.

-- name: GetTagForUser :one
SELECT * FROM tags WHERE id = @id AND owner_id = @owner_id;

-- name: LockTagForUser :one
SELECT * FROM tags WHERE id = @id AND owner_id = @owner_id FOR UPDATE;

-- name: TagExists :one
SELECT EXISTS (SELECT 1 FROM tags WHERE id = @id);

-- name: InsertTag :exec
INSERT INTO tags (id, owner_id, name, color, version, seq, created_at, updated_at)
VALUES (@id, @owner_id, @name, @color, 1, @seq, @now, @now);

-- name: UpdateTag :exec
UPDATE tags
SET name = @name, color = @color, version = @version, seq = @seq,
    updated_at = @updated_at, deleted_at = @deleted_at
WHERE id = @id;

-- name: ListLiveTags :many
SELECT * FROM tags WHERE owner_id = @owner_id AND deleted_at IS NULL ORDER BY lower(name), id;

-- name: LockLiveTags :many
-- Which of ids are live tags of the user. FOR SHARE keeps them from being
-- deleted until the task that references them is written.
SELECT id FROM tags
WHERE owner_id = @owner_id AND deleted_at IS NULL AND id = ANY(@ids::uuid[])
FOR SHARE;

-- name: LiveTagNameTaken :one
-- Whether another live tag of the user has this name, ignoring case.
SELECT EXISTS (
    SELECT 1 FROM tags
    WHERE owner_id = @owner_id AND deleted_at IS NULL AND lower(name) = lower(@name) AND id <> @id
);

-- name: LockTaskIDsWithTag :many
-- The user's tasks, live or deleted, that carry the tag.
SELECT t.id
FROM tasks t
JOIN list_members m ON m.list_id = t.list_id AND m.user_id = @user_id
WHERE t.tag_ids @> ARRAY[@tag_id::uuid]
ORDER BY t.id
FOR UPDATE OF t;

-- name: RemoveTagFromTasks :exec
UPDATE tasks t
SET tag_ids = array_remove(t.tag_ids, @tag_id::uuid), version = t.version + 1,
    seq = v.seq, updated_at = @now
-- Two unnest calls in one SELECT list advance together, pairing ids[i]
-- with seqs[i].
FROM (SELECT unnest(@ids::uuid[]) AS id, unnest(@seqs::bigint[]) AS seq) AS v
WHERE t.id = v.id;
