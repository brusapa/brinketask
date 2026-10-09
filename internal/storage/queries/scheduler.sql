-- The reminder scheduler (SPEC section 6). Every claim uses FOR UPDATE SKIP
-- LOCKED: two schedulers (two replicas, or two loops in a test) never take
-- the same row, and neither waits for the other (D-16).

-- name: ClaimDueReminders :many
SELECT * FROM reminders
WHERE next_fire_at <= @now AND deleted_at IS NULL
ORDER BY next_fire_at
LIMIT @lim
FOR UPDATE SKIP LOCKED;

-- name: ActiveSubscriptionsForTask :many
-- The devices of every member of the task's list.
SELECT s.id FROM push_subscriptions s
JOIN list_members m ON m.user_id = s.user_id
JOIN tasks t ON t.list_id = m.list_id
WHERE t.id = @task_id AND s.disabled_at IS NULL
ORDER BY s.id;

-- name: InsertDelivery :exec
-- ON CONFLICT: a firing already has its delivery for that device.
INSERT INTO notification_deliveries (reminder_id, fire_at, subscription_id, status, next_attempt_at, created_at)
VALUES (@reminder_id, @fire_at, @subscription_id, @status, @next_attempt_at, @now)
ON CONFLICT ON CONSTRAINT notification_deliveries_once DO NOTHING;

-- name: ClaimDueDelivery :one
-- One pending delivery that is due, with what its message needs.
SELECT d.id, d.reminder_id, d.fire_at, d.attempts,
       s.id AS subscription_id, s.endpoint, s.p256dh, s.auth,
       t.id AS task_id, t.title, t.due_date, t.due_time, t.due_tz,
       l.name AS list_name, l.is_inbox, u.timezone
FROM notification_deliveries d
JOIN push_subscriptions s ON s.id = d.subscription_id
JOIN reminders r ON r.id = d.reminder_id
JOIN tasks t ON t.id = r.task_id
JOIN lists l ON l.id = t.list_id
JOIN users u ON u.id = s.user_id
WHERE d.status = 'pending' AND d.next_attempt_at <= @now
ORDER BY d.next_attempt_at
LIMIT 1
FOR UPDATE OF d SKIP LOCKED;

-- name: MarkDeliverySent :exec
UPDATE notification_deliveries
SET status = 'sent', attempts = attempts + 1, sent_at = @now, next_attempt_at = NULL, error = NULL
WHERE id = @id;

-- name: MarkDeliveryRetry :exec
UPDATE notification_deliveries
SET attempts = attempts + 1, next_attempt_at = @next_attempt_at, error = @error
WHERE id = @id;

-- name: MarkDeliveryFailed :exec
UPDATE notification_deliveries
SET status = 'failed', attempts = attempts + 1, next_attempt_at = NULL, error = @error
WHERE id = @id;
