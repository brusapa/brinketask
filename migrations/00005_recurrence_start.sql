-- The start of a task's recurrence series (DTSTART, SPEC D-56). It is not
-- the current due date: after R-1 moves an occurrence from the 31st to the
-- 30th, the series must still know that the desired day is the 31st.
-- Internal: the API does not expose it.

-- +goose Up
ALTER TABLE tasks ADD COLUMN recurrence_start date;
ALTER TABLE tasks ADD CONSTRAINT tasks_rrule_needs_start
    CHECK (rrule IS NULL OR recurrence_start IS NOT NULL);
