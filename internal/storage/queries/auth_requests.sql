-- name: InsertAuthRequest :exec
INSERT INTO auth_requests (browser_hash, state, nonce, code_verifier, created_at, expires_at)
VALUES (@browser_hash, @state, @nonce, @code_verifier, @now, @expires_at);

-- name: ConsumeAuthRequest :one
-- Takes the login in progress of a browser and removes it in the same
-- statement, so each one can finish at most once.
DELETE FROM auth_requests WHERE browser_hash = @browser_hash RETURNING *;

-- name: DeleteExpiredAuthRequests :exec
-- Logins abandoned halfway; removed whenever a new one starts.
DELETE FROM auth_requests WHERE expires_at <= @now;
