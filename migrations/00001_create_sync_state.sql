-- +goose Up

-- Global sync counter (SPEC D-07). Every write to a syncable resource locks
-- this row and takes the next value as its seq, inside the same transaction,
-- so seq values become visible in order and the change cursor never skips
-- a concurrent write.
--
-- The boolean primary key constrained to true allows exactly one row.
CREATE TABLE sync_state (
    id  boolean PRIMARY KEY DEFAULT true CHECK (id),
    seq bigint  NOT NULL DEFAULT 0 CHECK (seq >= 0)
);

INSERT INTO sync_state (id, seq) VALUES (true, 0);
