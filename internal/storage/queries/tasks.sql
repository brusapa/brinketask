-- name: GetTaskForUser :one
-- A task in a list the user is a member of, live or deleted.
SELECT t.*
FROM tasks t
JOIN list_members m ON m.list_id = t.list_id AND m.user_id = @user_id
WHERE t.id = @id;

-- name: LockTaskForUser :one
SELECT t.*
FROM tasks t
JOIN list_members m ON m.list_id = t.list_id AND m.user_id = @user_id
WHERE t.id = @id
FOR UPDATE OF t;

-- name: TaskExists :one
-- Not filtered by user on purpose: an id taken by another user is a
-- conflict (D-23), so creation must see every row.
SELECT EXISTS (SELECT 1 FROM tasks WHERE id = @id);

-- name: LockListOfTaskForShare :one
-- Whether the list of one of the user's tasks is live, share-locked until
-- the transaction ends: restoring a task locks its list before the task,
-- as deleting the list does (parent before child), so the list cannot be
-- deleted between the check and the restore.
SELECT (l.deleted_at IS NULL)::boolean AS live
FROM lists l
JOIN tasks t ON t.list_id = l.id
JOIN list_members m ON m.list_id = l.id AND m.user_id = @user_id
WHERE t.id = @id
FOR SHARE OF l;

-- name: InsertTask :exec
-- Not filtered by user: the caller first locks the row this one belongs
-- to through a *ForUser query (list_members) in the same transaction.
INSERT INTO tasks (
    id, list_id, title, description, status, priority, position, due_date, due_time, due_tz,
    rrule, recurrence_start, repeat_from, tag_ids, version, seq, created_at, updated_at
) VALUES (
    @id, @list_id, @title, @description, 'open', @priority, @position, @due_date, @due_time, @due_tz,
    @rrule, @recurrence_start, @repeat_from, @tag_ids, 1, @seq, @now, @now
);

-- name: UpdateTask :exec
-- Not filtered by user: the caller first locks the row this one belongs
-- to through a *ForUser query (list_members) in the same transaction.
-- Writes every mutable column; the caller read the row under lock and
-- changed what the request asked for.
UPDATE tasks
SET list_id = @list_id, title = @title, description = @description, status = @status,
    priority = @priority, position = @position, due_date = @due_date, due_time = @due_time,
    due_tz = @due_tz, rrule = @rrule, recurrence_start = @recurrence_start, repeat_from = @repeat_from,
    recurrence_done_count = @recurrence_done_count, completed_at = @completed_at,
    tag_ids = @tag_ids, version = @version, seq = @seq, updated_at = @updated_at,
    deleted_at = @deleted_at
WHERE id = @id;

-- name: LiveChecklistItemsForTasks :many
-- Not filtered by user: the caller first locks the row this one belongs
-- to through a *ForUser query (list_members) in the same transaction.
SELECT * FROM checklist_items
WHERE task_id = ANY(@task_ids::uuid[]) AND deleted_at IS NULL
ORDER BY task_id, position, id;

-- name: QueryTasksByPosition :many
-- GET /tasks with sort=position: by the list's position, then the task's
-- (D-43). The filters are optional: a NULL argument disables its filter.
-- The keyset condition continues after the last row of the previous page.
SELECT sqlc.embed(t), l.position AS list_position
FROM tasks t
JOIN lists l ON l.id = t.list_id
JOIN list_members m ON m.list_id = t.list_id AND m.user_id = @user_id
WHERE CASE WHEN @trash::boolean
           THEN t.deleted_at > @restorable_since::timestamptz
                AND t.deleted_with_list_id IS NULL AND l.deleted_at IS NULL
           ELSE t.deleted_at IS NULL END
  AND (sqlc.narg(status)::text IS NULL OR t.status = sqlc.narg(status)::text)
  AND (sqlc.narg(list_id)::uuid IS NULL OR t.list_id = sqlc.narg(list_id)::uuid)
  AND (sqlc.narg(tag_id)::uuid IS NULL OR t.tag_ids @> ARRAY[sqlc.narg(tag_id)::uuid])
  AND (sqlc.narg(due_from)::date IS NULL OR t.due_date >= sqlc.narg(due_from)::date)
  AND (sqlc.narg(due_to)::date IS NULL OR t.due_date <= sqlc.narg(due_to)::date)
  AND (sqlc.narg(pattern)::text IS NULL
       OR unaccent(lower(t.title)) LIKE unaccent(lower(sqlc.narg(pattern)::text)) ESCAPE '\'
       OR unaccent(lower(t.description)) LIKE unaccent(lower(sqlc.narg(pattern)::text)) ESCAPE '\')
  AND (NOT @has_after::boolean
       OR (l.position, l.id, t.position, t.id)
          > (@after_list_position::text, @after_list_id::uuid, @after_position::text, @after_id::uuid))
ORDER BY l.position, l.id, t.position, t.id
LIMIT @lim;

-- name: QueryTasksByDue :many
-- GET /tasks with sort=due (D-43): no date last, all-day before timed. The
-- sort key replaces NULLs with values that sort the same way, because a
-- row comparison with a NULL is never true. The keyset date and time
-- arrive as text so the cursor can carry 'infinity'.
SELECT sqlc.embed(t), l.position AS list_position
FROM tasks t
JOIN lists l ON l.id = t.list_id
JOIN list_members m ON m.list_id = t.list_id AND m.user_id = @user_id
WHERE CASE WHEN @trash::boolean
           THEN t.deleted_at > @restorable_since::timestamptz
                AND t.deleted_with_list_id IS NULL AND l.deleted_at IS NULL
           ELSE t.deleted_at IS NULL END
  AND (sqlc.narg(status)::text IS NULL OR t.status = sqlc.narg(status)::text)
  AND (sqlc.narg(list_id)::uuid IS NULL OR t.list_id = sqlc.narg(list_id)::uuid)
  AND (sqlc.narg(tag_id)::uuid IS NULL OR t.tag_ids @> ARRAY[sqlc.narg(tag_id)::uuid])
  AND (sqlc.narg(due_from)::date IS NULL OR t.due_date >= sqlc.narg(due_from)::date)
  AND (sqlc.narg(due_to)::date IS NULL OR t.due_date <= sqlc.narg(due_to)::date)
  AND (sqlc.narg(pattern)::text IS NULL
       OR unaccent(lower(t.title)) LIKE unaccent(lower(sqlc.narg(pattern)::text)) ESCAPE '\'
       OR unaccent(lower(t.description)) LIKE unaccent(lower(sqlc.narg(pattern)::text)) ESCAPE '\')
  AND (NOT @has_after::boolean
       OR (coalesce(t.due_date, 'infinity'::date), t.due_time IS NOT NULL,
           coalesce(t.due_time, '00:00'::time), t.position, t.id)
          > ((@after_due_date::text)::date, @after_timed::boolean, (@after_due_time::text)::time,
             @after_position::text, @after_id::uuid))
ORDER BY coalesce(t.due_date, 'infinity'::date), t.due_time IS NOT NULL,
         coalesce(t.due_time, '00:00'::time), t.position, t.id
LIMIT @lim;
