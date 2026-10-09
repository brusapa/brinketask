-- The purge (SPEC section 8, D-71): resources deleted more than 90 days
-- ago go for good, with what depends on them, children before parents
-- because of the foreign keys. A row is purgeable when it, or the task it
-- belongs to, was deleted before @cutoff.

-- name: TryPurgeLock :one
-- One purge at a time across replicas. A transaction-level advisory lock
-- is released by the commit; the key is an arbitrary constant of this
-- application.
SELECT pg_try_advisory_xact_lock(7240618253::bigint);

-- name: PurgeDeliveries :execrows
-- Deliveries of purged reminders, and any delivery older than the cutoff.
DELETE FROM notification_deliveries d
WHERE d.created_at < @cutoff
   OR d.reminder_id IN (
        SELECT r.id FROM reminders r JOIN tasks t ON t.id = r.task_id
        WHERE r.deleted_at < @cutoff OR t.deleted_at < @cutoff);

-- name: PurgeReminders :many
DELETE FROM reminders r USING tasks t
WHERE t.id = r.task_id AND (r.deleted_at < @cutoff OR t.deleted_at < @cutoff)
RETURNING r.seq;

-- name: PurgeChecklistItems :many
DELETE FROM checklist_items c USING tasks t
WHERE t.id = c.task_id AND (c.deleted_at < @cutoff OR t.deleted_at < @cutoff)
RETURNING c.seq;

-- name: PurgeCompletions :execrows
-- Completion records go only with their task; a live task keeps them all
-- (SPEC section 1).
DELETE FROM task_completions c USING tasks t
WHERE t.id = c.task_id AND t.deleted_at < @cutoff;

-- name: PurgeTasks :many
DELETE FROM tasks WHERE deleted_at < @cutoff RETURNING seq;

-- name: PurgeListMembers :execrows
DELETE FROM list_members m USING lists l
WHERE l.id = m.list_id AND l.deleted_at < @cutoff
  AND NOT EXISTS (SELECT 1 FROM tasks t WHERE t.list_id = l.id);

-- name: PurgeLists :many
-- A list goes once its tasks are gone; deleting a list deletes its tasks
-- at the same moment (D-20), so they are purged together.
DELETE FROM lists l
WHERE l.deleted_at < @cutoff AND NOT EXISTS (SELECT 1 FROM tasks t WHERE t.list_id = l.id)
RETURNING l.seq;

-- name: PurgeTags :many
DELETE FROM tags WHERE deleted_at < @cutoff RETURNING seq;

-- name: RaisePurgedUpTo :exec
-- D-21: a sync cursor below this seq may have missed a purged tombstone.
UPDATE sync_state SET purged_up_to_seq = greatest(purged_up_to_seq, @seq::bigint);

-- name: PurgeExpiredSessions :execrows
DELETE FROM sessions WHERE expires_at <= @now;
