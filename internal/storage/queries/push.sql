-- Web Push subscriptions (devices). Not syncable; deleted for real (D-32).

-- name: ListSubscriptionsForUser :many
SELECT * FROM push_subscriptions WHERE user_id = @user_id ORDER BY created_at, id;

-- name: GetSubscriptionForUser :one
SELECT * FROM push_subscriptions WHERE id = @id AND user_id = @user_id;

-- name: LockSubscriptionByID :one
SELECT * FROM push_subscriptions WHERE id = @id FOR UPDATE;

-- name: LockSubscriptionByEndpoint :one
SELECT * FROM push_subscriptions WHERE endpoint = @endpoint FOR UPDATE;

-- name: InsertSubscription :exec
INSERT INTO push_subscriptions (id, user_id, channel, endpoint, p256dh, auth, label, created_at)
VALUES (@id, @user_id, @channel, @endpoint, @p256dh, @auth, @label, @now);

-- name: RefreshSubscription :exec
-- A device registering again: its keys may have changed, it may now belong
-- to another user (D-32), and it works again if it had been disabled.
UPDATE push_subscriptions
SET user_id = @user_id, p256dh = @p256dh, auth = @auth, label = @label, disabled_at = NULL
WHERE id = @id;

-- name: DeleteSubscriptionForUser :execrows
DELETE FROM push_subscriptions WHERE id = @id AND user_id = @user_id;

-- name: SubscriptionSucceeded :exec
UPDATE push_subscriptions SET last_success_at = @now WHERE id = @id;

-- name: DisableSubscription :exec
UPDATE push_subscriptions SET disabled_at = @now WHERE id = @id AND disabled_at IS NULL;
