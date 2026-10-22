-- The occurrence a delivery is about (SPEC D-66): the task's due fields
-- when its reminder fired. A retry sends these, not the task's current
-- ones, which may belong to the next occurrence by then; and it is
-- skipped once the task has moved past this date.

-- +goose Up
ALTER TABLE notification_deliveries
    ADD COLUMN occurrence_due_date date,
    ADD COLUMN occurrence_due_time time,
    ADD COLUMN occurrence_due_tz   text;

-- Deliveries still pending get the task's current fields, the best guess
-- for them; finished ones are history and keep nulls.
UPDATE notification_deliveries d
SET occurrence_due_date = t.due_date, occurrence_due_time = t.due_time, occurrence_due_tz = t.due_tz
FROM reminders r JOIN tasks t ON t.id = r.task_id
WHERE r.id = d.reminder_id AND d.status = 'pending';
