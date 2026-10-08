-- name: InsertSession :exec
INSERT INTO sessions (id_hash, user_id, created_at, last_seen_at, expires_at)
VALUES (@id_hash, @user_id, @now, @now, @expires_at);

-- name: GetSession :one
SELECT * FROM sessions WHERE id_hash = @id_hash;

-- name: TouchSession :exec
-- Records use of a session and slides its expiry (SPEC section 7).
UPDATE sessions SET last_seen_at = @now, expires_at = @expires_at WHERE id_hash = @id_hash;

-- name: DeleteSession :exec
DELETE FROM sessions WHERE id_hash = @id_hash;

-- name: DeleteExpiredSessions :exec
-- Expired sessions are useless; they are removed when their user logs in
-- again or when someone presents them.
DELETE FROM sessions WHERE user_id = @user_id AND expires_at <= @now;
