-- name: GetCompletion :one
SELECT * FROM task_completions WHERE id = @id;

-- name: LatestCompletionID :one
-- The record uncomplete may undo: the newest one not undone (D-39).
SELECT id FROM task_completions
WHERE task_id = @task_id AND undone_at IS NULL
ORDER BY task_seq DESC
LIMIT 1;

-- name: InsertCompletion :exec
INSERT INTO task_completions (
    id, task_id, kind, occurrence_due_date, completed_at, prev_due_date, prev_due_time,
    prev_recurrence_done_count, prev_status, task_seq
) VALUES (
    @id, @task_id, @kind, @occurrence_due_date, @completed_at, @prev_due_date, @prev_due_time,
    @prev_recurrence_done_count, @prev_status, @task_seq
);

-- name: MarkCompletionUndone :exec
UPDATE task_completions SET undone_at = @now WHERE id = @id;

-- name: ListTaskCompletions :many
-- A task's history, newest first, without undone records.
SELECT * FROM task_completions
WHERE task_id = @task_id AND undone_at IS NULL
  AND (sqlc.narg(before_seq)::bigint IS NULL OR task_seq < sqlc.narg(before_seq)::bigint)
ORDER BY task_seq DESC
LIMIT @lim;

-- name: ListCompletions :many
-- The Completed section (SPEC section 8): completed, not undone records of
-- the user's live tasks within [completed_from, completed_to), newest
-- first. can_undo marks the latest live record of its task.
SELECT sqlc.embed(c), t.list_id, t.title, t.priority, t.rrule,
       c.task_seq = (SELECT max(c2.task_seq) FROM task_completions c2
                     WHERE c2.task_id = c.task_id AND c2.undone_at IS NULL) AS can_undo
FROM task_completions c
JOIN tasks t ON t.id = c.task_id
JOIN list_members m ON m.list_id = t.list_id AND m.user_id = @user_id
WHERE c.kind = 'completed' AND c.undone_at IS NULL AND t.deleted_at IS NULL
  AND c.completed_at >= @completed_from AND c.completed_at < @completed_to
  AND (sqlc.narg(list_id)::uuid IS NULL OR t.list_id = sqlc.narg(list_id)::uuid)
  AND (sqlc.narg(tag_id)::uuid IS NULL OR t.tag_ids @> ARRAY[sqlc.narg(tag_id)::uuid])
  AND (sqlc.narg(due_from)::date IS NULL OR c.occurrence_due_date >= sqlc.narg(due_from)::date)
  AND (sqlc.narg(due_to)::date IS NULL OR c.occurrence_due_date <= sqlc.narg(due_to)::date)
  AND (NOT @has_after::boolean
       OR (c.completed_at, c.id) < (@after_completed_at::timestamptz, @after_id::uuid))
ORDER BY c.completed_at DESC, c.id DESC
LIMIT @lim;
