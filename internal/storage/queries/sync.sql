-- name: NextSeq :one
-- Takes the next value of the global change counter (D-07). The UPDATE
-- locks the single sync_state row until the transaction ends, so concurrent
-- writers take numbers one at a time and a reader never sees seq N+1
-- committed before seq N.
UPDATE sync_state SET seq = seq + 1 RETURNING seq;

-- name: ReserveSeqs :one
-- Takes @n consecutive values of the counter at once and returns the last
-- one; the caller uses last-n+1 .. last. Same lock as NextSeq. A write that
-- touches many rows (deleting a list deletes its tasks) gives each row its
-- own seq this way with one statement.
UPDATE sync_state SET seq = seq + @n::bigint RETURNING seq;

-- name: GetSyncState :one
SELECT seq, purged_up_to_seq FROM sync_state;
