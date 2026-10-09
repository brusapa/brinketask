-- name: GetReminderForUser :one
-- A reminder of a task in a list the user is a member of, with whether its
-- task is deleted.
SELECT sqlc.embed(r), (t.deleted_at IS NOT NULL)::boolean AS task_deleted
FROM reminders r
JOIN tasks t ON t.id = r.task_id
JOIN list_members m ON m.list_id = t.list_id AND m.user_id = @user_id
WHERE r.id = @id;

-- name: LockReminderForUser :one
SELECT sqlc.embed(r), (t.deleted_at IS NOT NULL)::boolean AS task_deleted
FROM reminders r
JOIN tasks t ON t.id = r.task_id
JOIN list_members m ON m.list_id = t.list_id AND m.user_id = @user_id
WHERE r.id = @id
FOR UPDATE OF r;

-- name: ReminderExists :one
SELECT EXISTS (SELECT 1 FROM reminders WHERE id = @id);

-- name: InsertReminder :exec
INSERT INTO reminders (id, task_id, kind, offset_minutes, at, next_fire_at, version, seq, created_at, updated_at)
VALUES (@id, @task_id, @kind, @offset_minutes, @at, @next_fire_at, 1, @seq, @now, @now);

-- name: UpdateReminder :exec
-- Writes every mutable column; the caller read the row under lock.
UPDATE reminders
SET offset_minutes = @offset_minutes, at = @at, next_fire_at = @next_fire_at,
    last_fired_at = @last_fired_at, version = @version, seq = @seq, updated_at = @updated_at,
    deleted_at = @deleted_at
WHERE id = @id;

-- name: LiveRemindersForTasks :many
SELECT * FROM reminders
WHERE task_id = ANY(@task_ids::uuid[]) AND deleted_at IS NULL
ORDER BY task_id, created_at, id;

-- name: CountLiveRemindersOfTask :one
-- The reminders that count towards the limit of 5 (snooze ones do not).
SELECT count(*)::integer FROM reminders
WHERE task_id = @task_id AND deleted_at IS NULL AND kind <> 'snooze';

-- name: LockLiveRemindersOfTask :many
-- A task's live reminders, locked, to recompute them after the task
-- changed. The caller holds the task's lock already.
SELECT * FROM reminders
WHERE task_id = @task_id AND deleted_at IS NULL
ORDER BY id
FOR UPDATE;

-- name: LockLiveRemindersOfUser :many
-- Every live reminder of the user's tasks, with its task, locked: changing
-- the profile zone or default time recomputes them (SPEC section 6).
SELECT sqlc.embed(r), sqlc.embed(t)
FROM reminders r
JOIN tasks t ON t.id = r.task_id
JOIN list_members m ON m.list_id = t.list_id AND m.user_id = @user_id
WHERE r.deleted_at IS NULL
ORDER BY r.id
FOR UPDATE OF r;
