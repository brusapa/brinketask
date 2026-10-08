-- name: GetChecklistItemForUser :one
-- An item of a task in a list the user is a member of, with whether its
-- task is deleted.
SELECT sqlc.embed(c), (t.deleted_at IS NOT NULL)::boolean AS task_deleted
FROM checklist_items c
JOIN tasks t ON t.id = c.task_id
JOIN list_members m ON m.list_id = t.list_id AND m.user_id = @user_id
WHERE c.id = @id;

-- name: LockChecklistItemForUser :one
SELECT sqlc.embed(c), (t.deleted_at IS NOT NULL)::boolean AS task_deleted
FROM checklist_items c
JOIN tasks t ON t.id = c.task_id
JOIN list_members m ON m.list_id = t.list_id AND m.user_id = @user_id
WHERE c.id = @id
FOR UPDATE OF c;

-- name: ChecklistItemExists :one
SELECT EXISTS (SELECT 1 FROM checklist_items WHERE id = @id);

-- name: InsertChecklistItem :exec
INSERT INTO checklist_items (id, task_id, title, is_done, position, version, seq, created_at, updated_at)
VALUES (@id, @task_id, @title, @is_done, @position, 1, @seq, @now, @now);

-- name: UpdateChecklistItem :exec
UPDATE checklist_items
SET title = @title, is_done = @is_done, position = @position, version = @version, seq = @seq,
    updated_at = @updated_at, deleted_at = @deleted_at
WHERE id = @id;
