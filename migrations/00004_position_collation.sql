-- Positions are fractional indexes compared by code point (SPEC D-12, D-51).
-- With the database's default collation, a locale such as en_US orders "a"
-- before "B", which is not what the client computes. COLLATE "C" compares
-- bytes, which for these ASCII strings is code point order, on any
-- database. Changing a column's collation rewrites its indexes; no index
-- covers position today.

-- +goose Up
ALTER TABLE lists           ALTER COLUMN position TYPE text COLLATE "C";
ALTER TABLE tasks           ALTER COLUMN position TYPE text COLLATE "C";
ALTER TABLE checklist_items ALTER COLUMN position TYPE text COLLATE "C";
