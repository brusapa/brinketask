-- name: NextSeq :one
-- Takes the next value of the global change counter (D-07). The UPDATE
-- locks the single sync_state row until the transaction ends, so concurrent
-- writers take numbers one at a time and a reader never sees seq N+1
-- committed before seq N.
UPDATE sync_state SET seq = seq + 1 RETURNING seq;
