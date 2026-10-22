-- Migration 00007 gave deliveries still pending the task's current due
-- fields as their occurrence. If the task had advanced since its reminder
-- fired, that is the next occurrence, and the retry would show, and
-- complete, it (D-10, D-66). Which occurrence those deliveries were about
-- is not known, so they are skipped.
--
-- Every delivery pending now is skipped, not only those 00007 filled in:
-- nothing in a row tells the two apart. When 00007 and this one run in the
-- same upgrade, as on any database upgraded from 6 at once, every pending
-- delivery is one of 00007's. On a database that ran 00007 earlier, the
-- cost is the retries in progress at this upgrade, a few minutes' worth
-- (D-66's waits add up to under 16 minutes).

-- +goose Up
UPDATE notification_deliveries
SET status = 'skipped', next_attempt_at = NULL, error = 'occurrence unknown after upgrade'
WHERE status = 'pending';
