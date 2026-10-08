-- name: UpsertUser :one
-- Signs a user in by OIDC identity (D-14): creates the row on the first
-- login and refreshes the informational claims on later ones. Either way
-- the row stays locked until the transaction ends, which serializes two
-- concurrent logins of the same user. updated_at only moves when a claim
-- changed.
INSERT INTO users (oidc_issuer, oidc_subject, email, display_name, created_at, updated_at)
VALUES (@oidc_issuer, @oidc_subject, @email, @display_name, @now, @now)
ON CONFLICT (oidc_issuer, oidc_subject) DO UPDATE
SET email        = EXCLUDED.email,
    display_name = EXCLUDED.display_name,
    updated_at   = CASE
        WHEN users.email IS DISTINCT FROM EXCLUDED.email
          OR users.display_name IS DISTINCT FROM EXCLUDED.display_name
        THEN EXCLUDED.updated_at
        ELSE users.updated_at
    END
RETURNING id;

-- name: GetUser :one
SELECT * FROM users WHERE id = @id;

-- name: GetInboxID :one
-- Membership-filtered like every data access (CLAUDE.md): the inbox the
-- user owns.
SELECT l.id
FROM lists l
JOIN list_members m ON m.list_id = l.id
WHERE m.user_id = @user_id AND m.role = 'owner' AND l.is_inbox AND l.owner_id = @user_id;

-- name: UpdateUserSettings :one
-- Merge patch of the profile settings (D-05): a NULL argument leaves its
-- column alone. updated_at only moves when a value actually changes.
UPDATE users
SET timezone              = coalesce(sqlc.narg(timezone), timezone),
    all_day_reminder_time = coalesce(sqlc.narg(all_day_reminder_time), all_day_reminder_time),
    updated_at = CASE
        WHEN coalesce(sqlc.narg(timezone), timezone) IS DISTINCT FROM timezone
          OR coalesce(sqlc.narg(all_day_reminder_time), all_day_reminder_time) IS DISTINCT FROM all_day_reminder_time
        THEN @now
        ELSE updated_at
    END
WHERE id = @id
RETURNING *;
