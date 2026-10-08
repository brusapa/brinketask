-- name: InsertList :exec
-- A new list at version 1. The caller takes seq from NextSeq in the same
-- transaction (D-07) and adds the owner's list_members row.
INSERT INTO lists (id, owner_id, name, color, position, is_inbox, version, seq, created_at, updated_at)
VALUES (@id, @owner_id, @name, @color, @position, @is_inbox, 1, @seq, @now, @now);

-- name: InsertListMember :exec
INSERT INTO list_members (list_id, user_id, role) VALUES (@list_id, @user_id, @role);
